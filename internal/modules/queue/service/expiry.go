package service

import (
	"context"
	"strings"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/log"
	legacyservices "queuebuzz/internal/services"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// SSE event type constants.
const (
	EventUserJoined         = "user_joined"
	EventUserLeft           = "user_left"
	EventUserStatusChanged  = "user_status_changed"
	EventUserCalled         = "user_called"
	EventQueueStatusChanged = "queue_status_changed"
	EventQueueUpdate        = "queue_update"
	EventQueueExpired       = "queue_expired"
)

// SSEMessage is the standard SSE event envelope. The Event field is
// extracted by the broker to set the SSE "event:" line.
type SSEMessage struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// UserStatusData is the payload for EventUserStatusChanged.
type UserStatusData struct {
	Token  string `json:"token"`
	Status string `json:"status"`
}

// QueueExpiredData is the payload for EventQueueExpired.
type QueueExpiredData struct {
	QueueID string `json:"queue_id"`
}

type QueueStatusData struct {
	Status string `json:"status"`
}

// ExpiryService manages Redis keyspace notifications and queue expiry cleanup.
// It lives in the queue module because it directly mutates queue/entry state.
type ExpiryService struct {
	rdb          *redis.Client
	queueCol     *mongo.Collection
	entryCol     *mongo.Collection
	redisService *legacyservices.RedisService
	notifier     *QueueNotifier
	notifSender  legacyservices.NotificationSender
}

// NewExpiryService creates a new expiry service.
func NewExpiryService(
	rdb *redis.Client,
	queueCol *mongo.Collection,
	entryCol *mongo.Collection,
	redisSvc *legacyservices.RedisService,
	notifier *QueueNotifier,
	notifSender legacyservices.NotificationSender,
) *ExpiryService {
	return &ExpiryService{
		rdb:          rdb,
		queueCol:     queueCol,
		entryCol:     entryCol,
		redisService: redisSvc,
		notifier:     notifier,
		notifSender:  notifSender,
	}
}

// StartKeyspaceListener subscribes to Redis keyspace notifications for expired keys.
// This runs in a goroutine and handles idle_timer, grace_timer, and queue expiry events.
func (s *ExpiryService) StartKeyspaceListener(ctx context.Context) {
	pubsub := s.rdb.PSubscribe(ctx, "__keyevent@0__:expired")

	go func() {
		defer pubsub.Close()

		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				s.handleExpiredKey(ctx, msg.Payload)
			}
		}
	}()

	log.Info().Msg("Redis keyspace expiry listener started")
}

func (s *ExpiryService) handleExpiredKey(ctx context.Context, key string) {
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

	// Move user to back of sorted set
	_ = s.redisService.MoveToBack(opCtx, queueID, token)

	// Set grace timer (5 min)
	_ = s.redisService.SetGraceTimer(opCtx, queueID, token,
		time.Duration(constants.DefaultGraceTimerSec)*time.Second)

	// Publish status change via SSE
	s.notifier.PublishUserStatus(queueID, token, constants.EntryStatusIdle)

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

	// Update status to SKIPPED
	_, err := s.entryCol.UpdateOne(opCtx,
		bson.M{"queue_id": queueID, "token": token},
		bson.M{"$set": bson.M{"status": constants.EntryStatusSkipped}},
	)
	if err != nil {
		log.Error().Err(err).Str("queue_id", queueID).Msg("Failed to set entry to SKIPPED")
		return
	}

	// Remove from sorted set
	_ = s.redisService.RemoveFromQueue(opCtx, queueID, token)

	// Publish via SSE
	s.notifier.PublishUserStatus(queueID, token, constants.EntryStatusSkipped)

	log.Info().Str("queue_id", queueID).Str("token_prefix", tokenPrefix(token)).Msg("User SKIPPED after grace period")
}

// RunCronSweep checks for expired queues and cleans them up.
// Acts as a fallback for missed Redis keyspace events.
func (s *ExpiryService) RunCronSweep(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepExpiredQueues(ctx)
		}
	}
}

func (s *ExpiryService) sweepExpiredQueues(ctx context.Context) {
	opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cursor, err := s.queueCol.Find(opCtx, bson.M{
		"status":     constants.QueueStatusActive,
		"expires_at": bson.M{"$lte": time.Now()},
	})
	if err != nil {
		log.Error().Err(err).Msg("Cron sweep: failed to find expired queues")
		return
	}

	type queueDoc struct {
		ID       string `bson:"_id"`
		JoinCode string `bson:"join_code"`
	}

	var expired []queueDoc
	if err := cursor.All(opCtx, &expired); err != nil {
		log.Error().Err(err).Msg("Cron sweep: failed to decode expired queues")
		return
	}

	for _, q := range expired {
		s.expireQueue(ctx, q.ID, q.JoinCode)
	}

	if len(expired) > 0 {
		log.Info().Int("count", len(expired)).Msg("Cron sweep: expired queues cleaned up")
	}
}

func (s *ExpiryService) expireQueue(ctx context.Context, queueID, joinCode string) {
	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 1. Update queue status to EXPIRED
	_, _ = s.queueCol.UpdateOne(opCtx,
		bson.M{"_id": queueID},
		bson.M{"$set": bson.M{"status": constants.QueueStatusExpired}},
	)

	// 2. Delete join code from Redis
	delCtx, delCancel := context.WithTimeout(ctx, 5*time.Second)
	defer delCancel()
	_ = s.rdb.Del(delCtx, "joincode:"+joinCode).Err()

	// 3. Publish queue expired event via SSE
	s.notifier.PublishQueueExpired(queueID)

	// 4. Clean up all Redis keys for this queue
	_ = s.redisService.DeleteQueueKeys(opCtx, queueID)

	// 5. Clear email fields from entries (privacy cleanup)
	_, _ = s.entryCol.UpdateMany(opCtx,
		bson.M{"queue_id": queueID},
		bson.M{"$unset": bson.M{"email": ""}},
	)

	log.Info().Str("queue_id", queueID).Msg("Queue expired and cleaned up")
}


func tokenPrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}
