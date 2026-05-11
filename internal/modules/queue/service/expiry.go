package service

import (
	"context"
	"strings"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/log"
	"queuebuzz/internal/modules/queue/domain"
	"queuebuzz/internal/modules/queue/repository"

	billingservice "queuebuzz/internal/modules/billing/service"
	systemservice "queuebuzz/internal/modules/system/service"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ExpiryService manages Redis keyspace notifications and queue expiry cleanup.
// It lives in the queue module because it directly mutates queue/entry state.
type ExpiryService struct {
	rdb        *redis.Client
	queueCol   *mongodriver.Collection
	entryCol   *mongodriver.Collection
	redisRepo  *repository.RedisRepository
	notifier   *QueueNotifier
	billingSvc *billingservice.BillingService
	systemSvc  *systemservice.SystemService
}

func NewExpiryService(
	rdb *redis.Client,
	queueCol *mongodriver.Collection,
	entryCol *mongodriver.Collection,
	redisRepo *repository.RedisRepository,
	notifier *QueueNotifier,
	billingSvc *billingservice.BillingService,
	systemSvc *systemservice.SystemService,
) *ExpiryService {
	return &ExpiryService{
		rdb:        rdb,
		queueCol:   queueCol,
		entryCol:   entryCol,
		redisRepo:  redisRepo,
		notifier:   notifier,
		billingSvc: billingSvc,
		systemSvc:  systemSvc,
	}
}

// Notifier returns the queue notifier.
func (s *ExpiryService) Notifier() *QueueNotifier {
	return s.notifier
}

// HandleExpiredKey processes a Redis key that just expired.
// It delegates to specific timer handlers based on the key prefix.
func (s *ExpiryService) HandleExpiredKey(ctx context.Context, key string) {
	switch {
	case strings.HasPrefix(key, "idle_timer:"):
		s.handleIdleTimerExpiry(ctx, key)
	case strings.HasPrefix(key, "grace_timer:"):
		s.handleGraceTimerExpiry(ctx, key)
	}
}

func (s *ExpiryService) handleIdleTimerExpiry(ctx context.Context, key string) {
	// key format: idle_timer:{queue_id}:{token}
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return
	}
	queueID, token := parts[1], parts[2]

	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Update status to IDLE
	_, err := s.entryCol.UpdateOne(opCtx,
		bson.M{"queue_id": queueID, "token": token},
		bson.M{"$set": bson.M{"status": constants.EntryStatusIdle}},
	)
	if err != nil {
		log.Error().Err(err).Str("queue_id", queueID).Msg("Failed to set entry to IDLE")
		return
	}

	// Settings for repositioning and grace timer
	repositionOffset := int64(constants.DefaultRepositionOffset)
	graceTimerSec := constants.DefaultGraceTimerSec
	settings, _ := s.systemSvc.GetSettings(opCtx)
	if settings != nil {
		if settings.DefaultQueueEntryRepositionOffset > 0 {
			repositionOffset = int64(settings.DefaultQueueEntryRepositionOffset)
		}
		if settings.DefaultQueueEntryGraceTimerSec > 0 {
			graceTimerSec = settings.DefaultQueueEntryGraceTimerSec
		}
	}

	// Move user back by the fair repositioning offset (e.g. 3 spots)
	_ = s.redisRepo.RepositionEntry(opCtx, queueID, token, repositionOffset)

	// Set grace timer (e.g. 5 min)
	_ = s.redisRepo.SetGraceTimer(opCtx, queueID, token,
		time.Duration(graceTimerSec)*time.Second)

	// Publish status change via SSE
	s.notifier.PublishUserStatus(queueID, token, constants.EntryStatusIdle)

	// Refresh positions (only for the affected first few rank shifted guests)
	s.BroadcastPositionsForQueue(ctx, queueID, repositionOffset+1)

	log.Info().Str("queue_id", queueID).Str("token_prefix", tokenPrefix(token)).Msg("User moved to IDLE")
}

func (s *ExpiryService) handleGraceTimerExpiry(ctx context.Context, key string) {
	// key format: grace_timer:{queue_id}:{token}
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return
	}
	queueID, token := parts[1], parts[2]

	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Update status to WAITING (Repositioned to back)
	_, err := s.entryCol.UpdateOne(opCtx,
		bson.M{"queue_id": queueID, "token": token},
		bson.M{"$set": bson.M{"status": constants.EntryStatusWaiting}},
	)
	if err != nil {
		log.Error().Err(err).Str("queue_id", queueID).Msg("Failed to set entry to WAITING during repositioning")
		return
	}

	// NOTE: We do NOT remove from sorted set here because they were moved
	// to the back during idle_timer_expiry and should stay there as WAITING.

	// Publish via SSE
	s.notifier.PublishUserStatus(queueID, token, constants.EntryStatusWaiting)

	// Refresh position ONLY for this specific user who reclaim waiting
	// Actually we can do a full refresh for small counts or target rank 4.
	repositionOffset := int64(constants.DefaultRepositionOffset)
	settings, _ := s.systemSvc.GetSettings(opCtx)
	if settings != nil && settings.DefaultQueueEntryRepositionOffset > 0 {
		repositionOffset = int64(settings.DefaultQueueEntryRepositionOffset)
	}
	s.BroadcastPositionsForQueue(ctx, queueID, repositionOffset+1)

	log.Info().Str("queue_id", queueID).Str("token_prefix", tokenPrefix(token)).Msg("User repositioned to WAITING after grace period")
}

// SweepExpiredQueues checks for expired queues and cleans them up.
func (s *ExpiryService) SweepExpiredQueues(ctx context.Context) {
	opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cursor, err := s.queueCol.Find(opCtx, bson.M{
		"status":     constants.QueueStatusActive,
		"expires_at": bson.M{"$lte": time.Now()},
	})
	if err != nil {
		log.Error().Err(err).Msg("Sweep expired queues: failed to find")
		return
	}

	type queueDoc struct {
		ID       string  `bson:"_id"`
		JoinCode string  `bson:"join_code"`
		HostID   *string `bson:"host_id"`
	}

	var expired []queueDoc
	if err := cursor.All(opCtx, &expired); err != nil {
		log.Error().Err(err).Msg("Sweep expired queues: failed to decode")
		return
	}

	for _, q := range expired {
		hostID := ""
		if q.HostID != nil {
			hostID = *q.HostID
		}
		s.expireQueue(ctx, q.ID, q.JoinCode, hostID)
	}

	if len(expired) > 0 {
		log.Info().Int("count", len(expired)).Msg("Cron sweep: expired queues cleaned up")
	}
}

// BroadcastAllActiveQueues updates positions for all currently active queues.
func (s *ExpiryService) BroadcastAllActiveQueues(ctx context.Context) {
	opCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cursor, err := s.queueCol.Find(opCtx, bson.M{
		"status": bson.M{"$in": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
	}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		log.Error().Err(err).Msg("Broadcaster: failed to find active queues")
		return
	}

	var queues []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(opCtx, &queues); err != nil {
		return
	}

	for _, q := range queues {
		s.BroadcastPositionsForQueue(ctx, q.ID, 0)
	}
}

// BroadcastPositionsForQueue updates positions for a specific queue.
// limit = 0 means broadcast to all waiting entries.
func (s *ExpiryService) BroadcastPositionsForQueue(ctx context.Context, queueID string, limit int64) {
	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var ids []string
	var err error

	if limit > 0 {
		ids, err = s.redisRepo.GetQueueEntryIDsRange(opCtx, queueID, 0, limit-1)
	} else {
		ids, err = s.redisRepo.GetQueueEntryIDs(opCtx, queueID)
	}

	if err != nil {
		return
	}

	s.notifier.PublishWaitingCount(queueID, int64(len(ids)))

	if len(ids) > 0 {
		s.notifier.PublishPositionUpdates(ids)
	}
}

func (s *ExpiryService) expireQueue(ctx context.Context, queueID, joinCode, hostID string) {
	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 1. Find unserved entries before we close the queue and clean up Redis
	filter := bson.M{
		"queue_id": queueID,
		"status": bson.M{"$in": []string{
			constants.EntryStatusWaiting,
			constants.EntryStatusCalled,
			constants.EntryStatusIdle,
			constants.EntryStatusArrived,
		}},
	}
	cursor, _ := s.entryCol.Find(opCtx, filter)
	var unserved []domain.Entry
	if cursor != nil {
		_ = cursor.All(opCtx, &unserved)
	}

	// 2. Get Queue for name
	var q domain.Queue
	_ = s.queueCol.FindOne(opCtx, bson.M{"_id": queueID}).Decode(&q)
	queueName := q.Name
	if queueName == "" {
		queueName = "Your Queue"
	}

	// 3. Notify unserved guests
	for _, entry := range unserved {
		s.notifier.NotifyQueueEnded(entry.ID, queueName)
	}

	// 4. Update queue status to EXPIRED
	now := time.Now()
	_, _ = s.queueCol.UpdateOne(opCtx,
		bson.M{"_id": queueID},
		bson.M{
			"$set": bson.M{
				"status":     constants.QueueStatusExpired,
				"closed_at":  now,
				"updated_at": now,
			},
			"$push": bson.M{
				"activity_logs": domain.ActivityLog{
					Type:      "STATUS_CHANGE",
					Value:     "EXPIRED",
					Timestamp: now,
				},
			},
		},
	)

	// 2. Delete join code from Redis
	_ = s.redisRepo.ReleaseJoinCode(ctx, joinCode)

	// 3. Publish queue expired event via SSE
	s.notifier.PublishQueueStatus(queueID, constants.QueueStatusExpired)

	// 4. Clean up all Redis keys for this queue
	_ = s.redisRepo.DeleteQueueKeys(opCtx, queueID)

	// 5. Plan-based Entry Cleanup
	// Free tier / Anonymous: Delete all entries entirely
	// Premium: Keep entries but unset PII (emails) for privacy after expiry
	plan, _ := s.billingSvc.GetHostPlan(opCtx, hostID)
	if plan == nil || plan.IsFree {
		_, _ = s.entryCol.DeleteMany(opCtx, bson.M{"queue_id": queueID})
		log.Info().Str("queue_id", queueID).Msg("Free tier queue entries deleted on expiry")
	} else {
		_, _ = s.entryCol.UpdateMany(opCtx,
			bson.M{
				"queue_id": queueID,
				"status": bson.M{"$in": []string{
					constants.EntryStatusWaiting,
					constants.EntryStatusCalled,
					constants.EntryStatusIdle,
					constants.EntryStatusArrived,
				}},
			},
			bson.M{
				"$set": bson.M{
					"status":      constants.EntryStatusSkipped,
					"finished_at": now,
				},
				"$unset": bson.M{"email": "", "phone": ""},
			},
		)
		log.Info().Str("queue_id", queueID).Msg("Premium queue entries marked as SKIPPED and PII cleared on expiry")
	}

	// Anonymous queues are hard-deleted after expiry so the record and guest PII are removed.
	if q.HostID == nil || *q.HostID == "" {
		_, _ = s.queueCol.DeleteOne(opCtx, bson.M{"_id": queueID})
	}

	if hostID != "" && s.redisRepo != nil {
		_ = s.redisRepo.InvalidateHostHistory(ctx, hostID)
		if q.HostPublicID != nil {
			_ = s.redisRepo.InvalidateHistorySummary(ctx, *q.HostPublicID)
		}
	}

	log.Info().Str("queue_id", queueID).Msg("Queue expired and cleaned up")
}

func tokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}
