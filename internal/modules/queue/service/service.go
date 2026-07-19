package service

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"regexp"
	"strings"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/log"
	"queuebuzz/internal/modules/queue/domain"
	"queuebuzz/internal/modules/queue/dto"
	"queuebuzz/internal/modules/queue/repository"

	"sort"

	billingservice "queuebuzz/internal/modules/billing/service"
	systemservice "queuebuzz/internal/modules/system/service"

	"github.com/google/uuid"
	"github.com/microcosm-cc/bluemonday"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// slugRegex enforces lowercase alphanumeric + hyphens, 3-30 chars,
// must start and end with alphanumeric.
var slugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,28}[a-z0-9]$`)

// generateVerifyCode produces a short human-readable code for verbal identity
// confirmation between host and customer. Uses an unambiguous charset (no 0/O/1/I/L).
func generateVerifyCode(length int) string {
	const charset = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, length)
	_, _ = cryptorand.Read(b)
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b)
}

var reservedSlugs = map[string]bool{
	"admin": true, "api": true, "app": true, "auth": true,
	"billing": true, "checkout": true, "dashboard": true,
	"guest-host": true, "help": true, "history": true,
	"join": true, "login": true, "pricing": true,
	"queue": true, "recover": true, "settings": true,
	"signup": true, "support": true, "terms": true,
	"privacy": true, "status": true, "waiting": true,
}

type Service struct {
	queueCol     *mongodriver.Collection
	entryCol     *mongodriver.Collection
	redisRepo    *repository.RedisRepository
	analyticsSvc *AnalyticsService
	billingSvc   *billingservice.BillingService
	systemSvc    *systemservice.SystemService
}

func New(
	queueCol *mongodriver.Collection,
	entryCol *mongodriver.Collection,
	redisRepo *repository.RedisRepository,
	analyticsSvc *AnalyticsService,
	billingSvc *billingservice.BillingService,
	systemSvc *systemservice.SystemService,
) *Service {
	return &Service{
		queueCol:     queueCol,
		entryCol:     entryCol,
		redisRepo:    redisRepo,
		analyticsSvc: analyticsSvc,
		billingSvc:   billingSvc,
		systemSvc:    systemSvc,
	}
}

// InvalidatePublicStatusSnapshot clears the short-lived public status cache for a queue.
func (s *Service) InvalidatePublicStatusSnapshot(ctx context.Context, queueID string) {
	if s.redisRepo == nil || queueID == "" {
		return
	}
	_ = s.redisRepo.InvalidatePublicStatusSnapshot(ctx, queueID)
}

// InvalidateDashboardCache clears the cached dashboard metrics for a host.
func (s *Service) InvalidateDashboardCache(hostPublicID string) {
	if s.analyticsSvc != nil && hostPublicID != "" {
		s.analyticsSvc.InvalidateDashboardCache(hostPublicID)
	}
}

type CreateQueueParams struct {
	HostID            *string
	HostPublicID      *string
	Name              string
	Slug              string
	AvgServiceMins    int
	AllowPartyJoining *bool
	MaxPartySize      *int
	ManualPositioning *bool
	IsGeoLocked       *bool
	Latitude          *float64
	Longitude         *float64
	GeoRadiusMeters   *float64
}

type JoinQueueParams struct {
	Queue       *domain.Queue
	Name        string
	Email       *string
	Phone       *string
	PartySize   *int
	FCMToken    *string
	CreatedBy   *string
	Fingerprint string
	Metadata    bson.M
}

type JoinQueueResult struct {
	domain.Entry
	Position int64 `json:"position"`
}

func (s *Service) HasCalledEntries(ctx context.Context, queueID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	filter := bson.M{
		"queue_id": queueID,
		"status":   constants.EntryStatusCalled,
	}

	count, err := s.entryCol.CountDocuments(ctx, filter)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

func (s *Service) CreateQueue(ctx context.Context, params CreateQueueParams) (*domain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if params.AvgServiceMins <= 0 {
		params.AvgServiceMins = constants.DefaultAvgServiceMins
	}

	slug := strings.ToLower(strings.TrimSpace(params.Slug))
	if !IsValidSlug(slug) {
		return nil, fmt.Errorf("invalid slug format: must be 3-30 lowercase alphanumeric characters or hyphens")
	}
	if reservedSlugs[slug] {
		return nil, fmt.Errorf("slug is reserved")
	}

	now := time.Now()

	allowParty := params.AllowPartyJoining != nil && *params.AllowPartyJoining
	maxParty := 0
	if params.MaxPartySize != nil {
		maxParty = *params.MaxPartySize
	}
	// Determine expiry from plan
	expiryHours := constants.DefaultQueueExpiryH
	hostID := ""
	if params.HostID != nil {
		hostID = *params.HostID
	}
	plan, _ := s.billingSvc.GetHostPlan(ctx, hostID)
	if plan != nil {
		if plan.Limits.QueueExpiryHours > 0 {
			expiryHours = plan.Limits.QueueExpiryHours
		} else if plan.Limits.QueueExpiryHours <= 0 && plan.Tier != "free" {
			// 0 = Unlimited (1 year technical limit)
			expiryHours = 24 * 365
		}
	}

	geoLocked := params.IsGeoLocked != nil && *params.IsGeoLocked
	var lat, lng, rad float64
	if params.Latitude != nil {
		lat = *params.Latitude
	}
	if params.Longitude != nil {
		lng = *params.Longitude
	}
	if params.GeoRadiusMeters != nil {
		rad = *params.GeoRadiusMeters
	}

	queue := domain.Queue{
		ID:                uuid.New().String(),
		HostID:            params.HostID,
		HostPublicID:      params.HostPublicID,
		Name:              params.Name,
		Slug:              slug,
		AvgServiceMins:    params.AvgServiceMins,
		AllowPartyJoining: allowParty,
		MaxPartySize:      maxParty,
		ManualPositioning: params.ManualPositioning != nil && *params.ManualPositioning,
		IsGeoLocked:       geoLocked,
		Latitude:          lat,
		Longitude:         lng,
		GeoRadiusMeters:   rad,
		Status:            constants.QueueStatusActive,
		CreatedAt:         now,
		UpdatedAt:         now,
		ExpiresAt:         now.Add(time.Duration(expiryHours) * time.Hour),
	}

	// Join Code settings
	settings, _ := s.systemSvc.GetSettings(ctx)
	codeLen := constants.JoinCodeLength
	maxAttempts := constants.MaxJoinCodeAttempts
	if settings != nil {
		if settings.DefaultQueueJoinCodeLength > 0 {
			codeLen = settings.DefaultQueueJoinCodeLength
		}
		if settings.DefaultQueueMaxJoinCodeAttempts > 0 {
			maxAttempts = settings.DefaultQueueMaxJoinCodeAttempts
		}
	}

	// Join code TTL should match queue expiry + extra buffer
	joinCodeTTL := time.Duration(expiryHours+constants.JoinCodeTTLExtraH) * time.Hour
	joinCode, err := s.redisRepo.GenerateJoinCode(ctx, queue.ID, codeLen, maxAttempts, joinCodeTTL)
	if err != nil {
		return nil, err
	}

	queue.JoinCode = joinCode
	queue.ActivityLogs = []domain.ActivityLog{
		{Type: "STATUS_CHANGE", Value: "CREATED", Timestamp: now},
	}
	if _, err := s.queueCol.InsertOne(ctx, queue); err != nil {
		return nil, fmt.Errorf("failed to create queue: %w", err)
	}

	// Increment monthly usage count for host
	if params.HostID != nil {
		_ = s.billingSvc.IncrementMonthlyQueueCount(ctx, *params.HostID)
	}

	// Warm up Redis re-hydration sentinel (avoid redundant first-load query to DB)
	_ = s.redisRepo.SetRehydratedSentinel(ctx, queue.ID, 1*time.Minute)

	// Invalidate history summary cache
	if params.HostPublicID != nil {
		_ = s.redisRepo.InvalidateHistorySummary(ctx, *params.HostPublicID)
		s.InvalidateDashboardCache(*params.HostPublicID)
	}

	return &queue, nil
}

// ExtendActiveQueueExpiry updates MongoDB and Redis for an active queue to match a host's new/current plan limits.
func (s *Service) ExtendActiveQueueExpiry(ctx context.Context, queueID string, hostID string) error {
	var queue domain.Queue
	err := s.queueCol.FindOne(ctx, bson.M{"_id": queueID}).Decode(&queue)
	if err != nil {
		return err
	}

	// Only extend active or paused queues
	if queue.Status != constants.QueueStatusActive && queue.Status != constants.QueueStatusPaused {
		return nil
	}

	plan, _ := s.billingSvc.GetHostPlan(ctx, hostID)
	expiryHours := constants.DefaultQueueExpiryH
	if plan != nil {
		if plan.Limits.QueueExpiryHours > 0 {
			expiryHours = plan.Limits.QueueExpiryHours
		} else if plan.Limits.QueueExpiryHours <= 0 && plan.Tier != "free" {
			expiryHours = 24 * 365
		}
	}

	newExpiresAt := queue.CreatedAt.Add(time.Duration(expiryHours) * time.Hour)
	if newExpiresAt.Before(time.Now()) {
		newExpiresAt = time.Now().Add(time.Duration(expiryHours) * time.Hour)
	}

	_, err = s.queueCol.UpdateOne(ctx, bson.M{"_id": queueID}, bson.M{
		"$set": bson.M{
			"expires_at": newExpiresAt,
			"updated_at": time.Now(),
		},
	})
	if err != nil {
		return err
	}

	ttl := time.Until(newExpiresAt)
	if ttl > 0 {
		_ = s.redisRepo.ExtendQueueKeysExpiry(ctx, queueID, queue.JoinCode, ttl)
	}

	return nil
}

func (s *Service) CheckSlugAvailability(ctx context.Context, slug string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	slug = strings.ToLower(strings.TrimSpace(slug))

	if !IsValidSlug(slug) {
		return false, fmt.Errorf("invalid slug format: must be 3-30 lowercase alphanumeric characters or hyphens")
	}
	if reservedSlugs[slug] {
		return false, nil
	}

	count, err := s.queueCol.CountDocuments(ctx, bson.M{"slug": slug})
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

// IsValidSlug checks slug format without DB lookup.
func IsValidSlug(slug string) bool {
	return slugRegex.MatchString(slug)
}

func (s *Service) GetQueue(ctx context.Context, queueID string) (*domain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var queue domain.Queue
	if err := s.queueCol.FindOne(ctx, bson.M{
		"_id":        queueID,
		"status":     bson.M{"$in": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
		"expires_at": bson.M{"$gt": time.Now()},
	}).Decode(&queue); err != nil {
		return nil, err
	}
	return &queue, nil
}

func (s *Service) TerminateQueue(ctx context.Context, queueID string) ([]domain.Entry, error) {
	now := time.Now()
	queue, qErr := s.GetQueue(ctx, queueID)

	update := bson.M{
		"$set": bson.M{
			"status":     constants.QueueStatusClosed,
			"closed_at":  now,
			"updated_at": now,
		},
		"$push": bson.M{
			"activity_logs": domain.ActivityLog{
				Type:      "STATUS_CHANGE",
				Value:     "COMPLETED",
				Timestamp: now,
			},
		},
	}

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
	cursor, err := s.entryCol.Find(ctx, filter)
	var unserved []domain.Entry
	if err == nil {
		_ = cursor.All(ctx, &unserved)
	}

	// 2. Update queue status
	_, err = s.queueCol.UpdateOne(ctx, bson.M{"_id": queueID}, update)
	if err == nil {
		if qErr == nil && queue != nil && queue.HostPublicID != nil {
			_ = s.redisRepo.InvalidateHistorySummary(ctx, *queue.HostPublicID)
			s.InvalidateDashboardCache(*queue.HostPublicID)
		}

		// 3. Mark unserved entries as SKIPPED
		if len(unserved) > 0 {
			_, _ = s.entryCol.UpdateMany(ctx, filter, bson.M{
				"$set": bson.M{
					"status":      constants.EntryStatusSkipped,
					"finished_at": now,
					"updated_at":  now,
				},
			})
		}

		// 4. Clean up Redis keys
		if qErr == nil && queue != nil {
			_ = s.redisRepo.DeleteQueueKeys(ctx, queueID)
			if queue.JoinCode != "" {
				_ = s.redisRepo.ReleaseJoinCode(ctx, queue.JoinCode)
			}
		}
	}
	return unserved, err
}

func (s *Service) PauseQueue(ctx context.Context, queueID string) error {
	return s.updateQueueStatus(ctx, queueID, constants.QueueStatusPaused)
}

func (s *Service) ResumeQueue(ctx context.Context, queueID string) error {
	return s.updateQueueStatus(ctx, queueID, constants.QueueStatusActive)
}

func (s *Service) UpdateQueue(ctx context.Context, queueID string, updates bson.M) (*domain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var queue domain.Queue
	updates["updated_at"] = time.Now()

	if notes, ok := updates["notes"].(string); ok {
		p := bluemonday.UGCPolicy()
		updates["notes"] = p.Sanitize(notes)
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err := s.queueCol.FindOneAndUpdate(ctx, bson.M{"_id": queueID}, bson.M{"$set": updates}, opts).Decode(&queue)
	if err != nil {
		return nil, err
	}
	if queue.HostPublicID != nil {
		s.InvalidateDashboardCache(*queue.HostPublicID)
	}
	return &queue, nil
}

func (s *Service) updateQueueStatus(ctx context.Context, queueID, status string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now := time.Now()
	update := bson.M{
		"$set": bson.M{"status": status, "updated_at": now},
		"$push": bson.M{
			"activity_logs": domain.ActivityLog{
				Type:      "STATUS_CHANGE",
				Value:     status,
				Timestamp: now,
			},
		},
	}
	_, err := s.queueCol.UpdateOne(ctx, bson.M{"_id": queueID}, update)
	if err == nil {
		if queue, qerr := s.GetQueue(ctx, queueID); qerr == nil && queue != nil && queue.HostPublicID != nil {
			s.InvalidateDashboardCache(*queue.HostPublicID)
		}
	}
	return err
}

func (s *Service) CreateEntry(ctx context.Context, entry domain.Entry) (*JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 1. Get Host context for identity isolation
	queue, err := s.GetQueue(ctx, entry.QueueID)
	if err != nil {
		return nil, fmt.Errorf("queue not found: %w", err)
	}

	// 2. Process Identity & Returning Status
	s.analyticsSvc.ProcessIdentity(ctx, &entry, queue)

	// 3. Capacity Checks are now handled by GuestCapacityGuard middleware

	// 4. Standard entry creation
	if err := s.ensureTicketCounter(ctx, entry.QueueID); err != nil {
		return nil, err
	}

	// Join the queue with dynamic TTL
	ttl := time.Until(queue.ExpiresAt)
	if ttl <= 0 {
		ttl = 24 * time.Hour // fallback
	}

	ticketNo, err := s.redisRepo.NextTicket(ctx, entry.QueueID, ttl)
	if err != nil {
		return nil, err
	}
	entry.TicketNo = ticketNo

	now := time.Now()
	const maxVerifyCodeAttempts = 5
	inserted := false

	for attempt := 0; attempt < maxVerifyCodeAttempts; attempt++ {
		candidate := entry
		candidate.ID = uuid.New().String()
		candidate.Token = uuid.New().String()
		candidate.VerifyCode = generateVerifyCode(6)
		candidate.Status = constants.EntryStatusWaiting
		candidate.CreatedAt = now
		candidate.UpdatedAt = now

		if _, err := s.entryCol.InsertOne(ctx, candidate); err != nil {
			if mongodriver.IsDuplicateKeyError(err) {
				log.Warn().Int("attempt", attempt+1).Str("queue_id", entry.QueueID).Msg("verify code collision resolved")
				continue
			}
			return nil, fmt.Errorf("failed to insert queue entry: %w", err)
		}

		entry = candidate
		inserted = true
		break
	}

	if !inserted {
		return nil, fmt.Errorf("failed to generate unique verify code after %d attempts", maxVerifyCodeAttempts)
	}

	var position int64
	if !queue.ManualPositioning {
		score := float64(now.UnixMilli())
		if err := s.redisRepo.AddToQueue(ctx, entry.QueueID, entry.ID, score, ttl); err != nil {
			return nil, fmt.Errorf("failed to add to queue positions: %w", err)
		}

		pos, err := s.GetPosition(ctx, entry.QueueID, entry.ID)
		if err == nil {
			position = pos + 1
		}
	}

	if queue.HostPublicID != nil {
		s.InvalidateDashboardCache(*queue.HostPublicID)
	}

	return &JoinQueueResult{
		Entry:    entry,
		Position: position,
	}, nil
}

func (s *Service) GetQueueEntries(ctx context.Context, queueID string, statuses ...string) ([]domain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if len(statuses) == 0 {
		statuses = []string{
			constants.EntryStatusWaiting,
			constants.EntryStatusCalled,
			constants.EntryStatusIdle,
			constants.EntryStatusArrived,
		}
	}

	filter := bson.M{
		"queue_id": queueID,
		"status":   bson.M{"$in": statuses},
	}

	cursor, err := s.entryCol.Find(ctx, filter, options.Find().SetSort(bson.M{"created_at": 1}))
	if err != nil {
		return nil, err
	}

	var entries []domain.Entry
	if err := cursor.All(ctx, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// GetWaitingEntryIDs returns only the IDs of entries currently in the queue,
// sorted by arrival time. Used for high-performance position broadcasting.
func (s *Service) GetWaitingEntryIDs(ctx context.Context, queueID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	filter := bson.M{
		"queue_id": queueID,
		"status":   bson.M{"$in": []string{constants.EntryStatusWaiting, constants.EntryStatusCalled}},
	}
	opts := options.Find().SetProjection(bson.M{"_id": 1}).SetSort(bson.M{"created_at": 1})

	cursor, err := s.entryCol.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}

	var results []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	return ids, nil
}

func (s *Service) GetEntry(ctx context.Context, entryID string) (*domain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var entry domain.Entry
	if err := s.entryCol.FindOne(ctx, bson.M{"_id": entryID}).Decode(&entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (s *Service) UpdateEntryStatus(ctx context.Context, entryID, status string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_, err := s.entryCol.UpdateOne(ctx, bson.M{"_id": entryID}, bson.M{"$set": bson.M{"status": status, "updated_at": time.Now()}})
	return err
}

func (s *Service) CallNextUser(ctx context.Context, queueID string) (*domain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	entryIDs, err := s.redisRepo.GetQueueEntryIDs(ctx, queueID)
	if err != nil {
		return nil, err
	}

	// Lazy Re-hydration: If Redis is empty, check MongoDB
	if len(entryIDs) == 0 {
		rehydratedIDs, reerr := s.rehydrateQueue(ctx, queueID)
		if reerr != nil {
			return nil, reerr
		}
		entryIDs = rehydratedIDs
	}

	if len(entryIDs) == 0 {
		return nil, fmt.Errorf("no users in queue")
	}
	for _, entryID := range entryIDs {
		entry, err := s.GetEntry(ctx, entryID)
		if err != nil {
			continue
		}
		if entry.Status == constants.EntryStatusWaiting {
			if err := s.UpdateEntryStatus(ctx, entryID, constants.EntryStatusCalled); err != nil {
				return nil, err
			}
			entry.Status = constants.EntryStatusCalled

			// Start Idle Timer (Guest has X mins to show up)
			idleMins := constants.DefaultIdleTimeoutMin
			settings, _ := s.systemSvc.GetSettings(ctx)
			if settings != nil && settings.DefaultQueueEntryIdleTimeoutMin > 0 {
				idleMins = settings.DefaultQueueEntryIdleTimeoutMin
			}
			_ = s.redisRepo.SetIdleTimer(ctx, queueID, entry.ID, time.Duration(idleMins)*time.Minute)

			if q, qerr := s.GetQueue(ctx, queueID); qerr == nil && q != nil && q.HostPublicID != nil {
				s.InvalidateDashboardCache(*q.HostPublicID)
			}

			return entry, nil
		}
	}
	return nil, fmt.Errorf("no waiting users in queue")
}

func (s *Service) RemoveUser(ctx context.Context, entryID string) error {
	return s.finishUserSession(ctx, entryID, constants.EntryStatusLeft)
}

func (s *Service) ServeUser(ctx context.Context, entryID string) error {
	return s.finishUserSession(ctx, entryID, constants.EntryStatusServed)
}

func (s *Service) SkipUser(ctx context.Context, entryID string) error {
	return s.finishUserSession(ctx, entryID, constants.EntryStatusSkipped)
}

func (s *Service) finishUserSession(ctx context.Context, entryID, status string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	now := time.Now()
	update := bson.M{
		"$set": bson.M{
			"status":      status,
			"finished_at": now,
		},
	}
	if status == constants.EntryStatusServed {
		update["$set"].(bson.M)["served_at"] = now
	}

	var entry domain.Entry
	if err := s.entryCol.FindOne(ctx, bson.M{"_id": entryID}).Decode(&entry); err != nil {
		return err
	}

	_, err := s.entryCol.UpdateOne(ctx, bson.M{"_id": entryID}, update)
	if err != nil {
		return err
	}
	_ = s.redisRepo.RemoveFromQueue(ctx, entry.QueueID, entryID)

	// Update host metrics and invalidate cache
	var q domain.Queue
	if err := s.queueCol.FindOne(ctx, bson.M{"_id": entry.QueueID}).Decode(&q); err == nil {
		if q.HostPublicID != nil {
			s.InvalidateDashboardCache(*q.HostPublicID)
		}
		if status == constants.EntryStatusServed && q.HostID != nil {
			_ = s.billingSvc.IncrementTotalServedCount(ctx, *q.HostID)
		}
	}

	return nil
}

func (s *Service) GetEntryStatusByID(ctx context.Context, entryID string) (*JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var entry domain.Entry
	if err := s.entryCol.FindOne(ctx, bson.M{"_id": entryID}).Decode(&entry); err != nil {
		return nil, err
	}

	queue, err := s.GetQueue(ctx, entry.QueueID)
	if err != nil {
		return nil, fmt.Errorf("queue not found: %w", err)
	}

	var position int64
	if !queue.ManualPositioning {
		pos, err := s.GetPosition(ctx, entry.QueueID, entry.ID)
		if err == nil {
			position = pos + 1
		}
	}

	return &JoinQueueResult{
		Entry:    entry,
		Position: position,
	}, nil
}

func (s *Service) GetPosition(ctx context.Context, queueID, entryID string) (int64, error) {
	pos, err := s.redisRepo.GetPosition(ctx, queueID, entryID)
	if err != nil || pos == -1 {
		// Only attempt re-hydration if the entire queue is missing from Redis
		size, _ := s.redisRepo.GetSize(ctx, queueID)
		if size == 0 {
			// Check if we already re-hydrated recently (avoid DB thundering herd)
			rehydrated, _ := s.redisRepo.HasRehydratedSentinel(ctx, queueID)
			if !rehydrated {
				_, reerr := s.rehydrateQueue(ctx, queueID)
				if reerr == nil {
					pos, err = s.redisRepo.GetPosition(ctx, queueID, entryID)
				}
			}
		}
	}
	return pos, err
}

func (s *Service) GetWaitingCount(ctx context.Context, queueID string) (int64, error) {
	count, err := s.redisRepo.GetSize(ctx, queueID)
	if err == nil && count == 0 {
		// Potential wipe — try to rehydrate if not recently synced
		rehydrated, _ := s.redisRepo.HasRehydratedSentinel(ctx, queueID)
		if !rehydrated {
			ids, _ := s.rehydrateQueue(ctx, queueID)
			count = int64(len(ids))
		}
	}
	return count, err
}

func (s *Service) GetActiveQueuesForHost(ctx context.Context, hostPublicID string) ([]domain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cursor, err := s.queueCol.Find(ctx, bson.M{"host_public_id": hostPublicID, "status": constants.QueueStatusActive})
	if err != nil {
		return nil, err
	}
	var queues []domain.Queue
	if err := cursor.All(ctx, &queues); err != nil {
		return nil, err
	}
	return queues, nil
}

func (s *Service) CountQueuesCreatedSince(ctx context.Context, hostID string, since time.Time) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter := bson.M{
		"host_id":    hostID,
		"created_at": bson.M{"$gte": since},
	}

	return s.queueCol.CountDocuments(ctx, filter)
}

type QueueWithStats struct {
	domain.Queue `bson:",inline"`
	TotalServed  int   `bson:"total_served"`
	AvgWaitMS    int64 `bson:"avg_wait_ms"`
}

func (s *Service) GetQueueHistoryListForHost(ctx context.Context, hostID, hostPublicID string, search, status string, page, limit int) ([]QueueWithStats, int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if limit <= 0 {
		limit = 10
	}
	if page <= 0 {
		page = 1
	}

	match := bson.M{
		"host_public_id": hostPublicID,
		"status":         bson.M{"$nin": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
	}

	// Apply History Retention limit
	retentionDays, _ := s.billingSvc.GetHistoryRetentionDays(ctx, hostID)
	if retentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -retentionDays)
		match["created_at"] = bson.M{"$gte": cutoff}
	}

	if search != "" {
		match["name"] = bson.M{"$regex": search, "$options": "i"}
	}
	if status != "" && status != "all" {
		statusUpper := strings.ToUpper(status)
		switch statusUpper {
		case "COMPLETED", "TERMINATED":
			match["status"] = constants.QueueStatusClosed
		default:
			match["status"] = statusUpper
		}
	}

	pipeline := mongodriver.Pipeline{
		{{Key: "$match", Value: match}},
		{{Key: "$facet", Value: bson.M{
			"metadata": bson.A{
				bson.M{"$count": "total"},
			},
			"data": bson.A{
				bson.M{"$sort": bson.M{"created_at": -1}},
				bson.M{"$skip": (page - 1) * limit},
				bson.M{"$limit": limit},
				bson.M{"$lookup": bson.M{
					"from": "queue_entries",
					"let":  bson.M{"queue_id": "$_id"},
					"pipeline": bson.A{
						bson.M{"$match": bson.M{
							"$expr":  bson.M{"$eq": bson.A{"$queue_id", "$$queue_id"}},
							"status": constants.EntryStatusServed,
						}},
					},
					"as": "served_entries",
				}},
				bson.M{"$addFields": bson.M{
					"total_served": bson.M{"$size": "$served_entries"},
					"avg_wait_ms": bson.M{
						"$cond": bson.A{
							bson.M{"$gt": bson.A{bson.M{"$size": "$served_entries"}, 0}},
							bson.M{"$avg": bson.M{
								"$map": bson.M{
									"input": "$served_entries",
									"as":    "e",
									"in":    bson.M{"$subtract": bson.A{"$$e.served_at", "$$e.created_at"}},
								},
							}},
							0,
						},
					},
				}},
				bson.M{"$project": bson.M{"served_entries": 0}},
			},
		}}},
	}

	cursor, err := s.queueCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		Metadata []struct {
			Total int64 `bson:"total"`
		} `bson:"metadata"`
		Data []QueueWithStats `bson:"data"`
	}

	if err := cursor.All(ctx, &results); err != nil {
		return nil, 0, err
	}

	if len(results) == 0 {
		return []QueueWithStats{}, 0, nil
	}

	total := int64(0)
	if len(results[0].Metadata) > 0 {
		total = results[0].Metadata[0].Total
	}

	return results[0].Data, total, nil
}

func (s *Service) GetHostHistorySummary(ctx context.Context, hostID, hostPublicID string) (*domain.HostHistorySummary, error) {
	// 1. Try cache
	if summary, err := s.redisRepo.GetHistorySummary(ctx, hostPublicID); err == nil && summary != nil {
		return summary, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pipeline := mongodriver.Pipeline{
		// 1. Match queues for this host that are not active/paused
		{{Key: "$match", Value: (func() bson.M {
			match := bson.M{
				"host_public_id": hostPublicID,
				"status":         bson.M{"$nin": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
			}
			retentionDays, _ := s.billingSvc.GetHistoryRetentionDays(ctx, hostID)
			if retentionDays > 0 {
				cutoff := time.Now().AddDate(0, 0, -retentionDays)
				match["created_at"] = bson.M{"$gte": cutoff}
			}
			return match
		})()}},
		// 2. Lookup served entries
		{{Key: "$lookup", Value: bson.M{
			"from": "queue_entries",
			"let":  bson.M{"queue_id": "$_id"},
			"pipeline": bson.A{
				bson.M{"$match": bson.M{
					"$expr":  bson.M{"$eq": bson.A{"$queue_id", "$$queue_id"}},
					"status": constants.EntryStatusServed,
				}},
			},
			"as": "served_entries",
		}}},
		// 3. Group everything
		{{Key: "$group", Value: bson.M{
			"_id":            nil,
			"total_sessions": bson.M{"$sum": 1},
			"total_served":   bson.M{"$sum": bson.M{"$size": "$served_entries"}},
			"total_wait_ms": bson.M{"$sum": bson.M{
				"$sum": bson.M{
					"$map": bson.M{
						"input": "$served_entries",
						"as":    "e",
						"in":    bson.M{"$subtract": bson.A{"$$e.served_at", "$$e.created_at"}},
					},
				},
			}},
		}}},
	}

	cursor, err := s.queueCol.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []domain.HostHistorySummary
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	var summary *domain.HostHistorySummary
	if len(results) == 0 {
		summary = &domain.HostHistorySummary{}
	} else {
		summary = &results[0]
	}

	// 4. Cache it
	_ = s.redisRepo.SetHistorySummary(ctx, hostPublicID, summary)

	return summary, nil
}

func (s *Service) GetLiveQueueForHost(ctx context.Context, hostPublicID string) (*domain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var queue domain.Queue
	if err := s.queueCol.FindOne(ctx, bson.M{
		"host_public_id": hostPublicID,
		"status":         bson.M{"$in": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
		"expires_at":     bson.M{"$gt": time.Now()},
	}).Decode(&queue); err != nil {
		if err == mongodriver.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &queue, nil
}

func (s *Service) GetLiveQueueByID(ctx context.Context, queueID string) (*domain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var queue domain.Queue

	filter := bson.M{
		"$or": []bson.M{
			{"_id": queueID},
			{"slug": queueID},
		},
		"status":     bson.M{"$in": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
		"expires_at": bson.M{"$gt": time.Now()},
	}

	if err := s.queueCol.FindOne(ctx, filter).Decode(&queue); err != nil {
		if err == mongodriver.ErrNoDocuments {
			hostsCol := s.queueCol.Database().Collection("hosts")
			var host struct {
				ID       string `bson:"_id"`
				PublicID string `bson:"public_id"`
			}
			hostFilter := bson.M{
				"$or": []bson.M{
					{"_id": queueID},
					{"public_id": queueID},
					{"slug": queueID},
				},
			}
			if err := hostsCol.FindOne(ctx, hostFilter).Decode(&host); err == nil {
				var liveQueue domain.Queue
				if err := s.queueCol.FindOne(ctx, bson.M{
					"host_public_id": host.PublicID,
					"status":         bson.M{"$in": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
					"expires_at":     bson.M{"$gt": time.Now()},
				}).Decode(&liveQueue); err == nil {
					return &liveQueue, nil
				}
			}
			return nil, nil
		}
		return nil, err
	}
	return &queue, nil
}

func (s *Service) HasActiveQueue(ctx context.Context, hostPublicID, queueID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var result struct {
		ID string `bson:"_id"`
	}

	var filter bson.M
	if hostPublicID != "" {
		filter = bson.M{
			"host_public_id": hostPublicID,
			"status":         bson.M{"$in": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
			"expires_at":     bson.M{"$gt": time.Now()},
		}
	} else if queueID != "" {
		filter = bson.M{
			"$or": []bson.M{
				{"_id": queueID},
				{"slug": queueID},
			},
			"status":     bson.M{"$in": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
			"expires_at": bson.M{"$gt": time.Now()},
		}
	} else {
		return "", nil
	}

	opts := options.FindOne().SetProjection(bson.M{"_id": 1})
	err := s.queueCol.FindOne(ctx, filter, opts).Decode(&result)
	if err != nil {
		if err == mongodriver.ErrNoDocuments {
			return "", nil
		}
		return "", err
	}
	return result.ID, nil
}

func (s *Service) GetActiveQueueByJoinCode(ctx context.Context, joinCode string) (*domain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var queue domain.Queue
	if err := s.queueCol.FindOne(ctx, bson.M{
		"join_code":  joinCode,
		"status":     bson.M{"$in": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
		"expires_at": bson.M{"$gt": time.Now()},
	}).Decode(&queue); err != nil {
		if err == mongodriver.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &queue, nil
}

type QueueStats struct {
	TotalServed int
	AvgWait     time.Duration
}

func (s *Service) GetQueueStats(ctx context.Context, queueID string) (*QueueStats, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter := bson.M{"queue_id": queueID, "status": constants.EntryStatusServed}
	cursor, err := s.entryCol.Find(ctx, filter)
	if err != nil {
		return nil, err
	}

	var entries []domain.Entry
	if err := cursor.All(ctx, &entries); err != nil {
		return nil, err
	}

	stats := &QueueStats{
		TotalServed: len(entries),
	}

	if len(entries) > 0 {
		var totalWait time.Duration
		for _, e := range entries {
			if e.ServedAt != nil {
				totalWait += e.ServedAt.Sub(e.CreatedAt)
			}
		}
		stats.AvgWait = totalWait / time.Duration(len(entries))
	}

	return stats, nil
}

func (s *Service) RegisterHostFCM(ctx context.Context, queueID, fcmToken string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := s.queueCol.UpdateOne(
		ctx,
		bson.M{"_id": queueID},
		bson.M{
			"$set": bson.M{
				"host_fcm_token":      fcmToken,
				"host_fcm_updated_at": time.Now(),
			},
		},
	)
	return err
}

func (s *Service) UnregisterHostFCM(ctx context.Context, queueID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := s.queueCol.UpdateOne(
		ctx,
		bson.M{"_id": queueID},
		bson.M{
			"$unset": bson.M{
				"host_fcm_token":      "",
				"host_fcm_updated_at": "",
			},
		},
	)
	return err
}

func (s *Service) ClearHostHistory(ctx context.Context, hostPublicID string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 1. Get all queue IDs for this host
	cursor, err := s.queueCol.Find(ctx, bson.M{"host_public_id": hostPublicID})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var queueIDs []string
	for cursor.Next(ctx) {
		var q struct {
			ID string `bson:"_id"`
		}
		if err := cursor.Decode(&q); err == nil {
			queueIDs = append(queueIDs, q.ID)
		}
	}

	// 2. Delete all entries for these queues
	if len(queueIDs) > 0 {
		_, _ = s.entryCol.DeleteMany(ctx, bson.M{"queue_id": bson.M{"$in": queueIDs}})
	}

	// 3. Delete all queues for this host
	_, err = s.queueCol.DeleteMany(ctx, bson.M{"host_public_id": hostPublicID})
	if err == nil {
		// Invalidate cache
		_ = s.redisRepo.InvalidateHistorySummary(ctx, hostPublicID)
		s.InvalidateDashboardCache(hostPublicID)
	}

	return err
}

/**
 * RE-HYDRATION LOGIC
 * These methods recover Redis state from MongoDB in case of a Redis restart/wipe.
 */
func (s *Service) ensureTicketCounter(ctx context.Context, queueID string) error {
	// If counter exists, do nothing
	exists, err := s.redisRepo.HasTicketCounter(ctx, queueID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Counter missing — find highest ticket in DB
	filter := bson.M{"queue_id": queueID}
	opts := options.FindOne().SetSort(bson.M{"ticket_no": -1}).SetProjection(bson.M{"ticket_no": 1})

	var lastEntry struct {
		TicketNo string `bson:"ticket_no"`
	}
	err = s.entryCol.FindOne(ctx, filter, opts).Decode(&lastEntry)

	var lastNum int64
	if err == nil {
		if _, parseErr := fmt.Sscanf(lastEntry.TicketNo, "Q-%04d", &lastNum); parseErr != nil {
			log.Warn().Err(parseErr).Str("ticket_no", lastEntry.TicketNo).Msg("Failed to parse ticket number from last entry")
		}
	}

	// Sync Redis with DB last known ticket
	log.Debug().Str("queue_id", queueID).Int64("last_num", lastNum).Msg("Redis: re-hydrated ticket counter from MongoDB")
	ttl := s.getQueueRedisTTL(ctx, queueID)
	return s.redisRepo.SetTicketCounter(ctx, queueID, lastNum, ttl)
}

func (s *Service) rehydrateQueue(ctx context.Context, queueID string) ([]string, error) {
	// 1. Mark as re-hydrated immediately/early to throttle concurrent attempts
	_ = s.redisRepo.SetRehydratedSentinel(ctx, queueID, 1*time.Minute)

	// 2. Fetch live entries from MongoDB
	entries, err := s.GetQueueEntries(ctx, queueID,
		constants.EntryStatusWaiting,
		constants.EntryStatusCalled,
		constants.EntryStatusArrived,
	)
	if err != nil || len(entries) == 0 {
		return nil, err
	}

	log.Info().Str("queue_id", queueID).Int("count", len(entries)).Msg("Redis: re-hydrating live queue from MongoDB")

	// 3. Re-populate Redis in a single bulk operation
	members := make([]redis.Z, len(entries))
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
		members[i] = redis.Z{
			Score:  float64(entry.CreatedAt.UnixMilli()),
			Member: entry.ID,
		}
	}

	ttl := s.getQueueRedisTTL(ctx, queueID)
	_ = s.redisRepo.AddToQueueBulk(ctx, queueID, members, ttl)

	return ids, nil
}

func (s *Service) getQueueRedisTTL(ctx context.Context, queueID string) time.Duration {
	queue, err := s.GetQueue(ctx, queueID)
	if err != nil || queue == nil {
		return 24 * time.Hour
	}
	ttl := time.Until(queue.ExpiresAt)
	if ttl <= 0 {
		return 24 * time.Hour
	}
	// Add 2h buffer to ensure Redis keys outlive the MongoDB expiry sweep
	return ttl + 2*time.Hour
}

func (s *Service) GetHistoryDetail(ctx context.Context, queueID string) (*dto.HistoryDetailResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 1. Fetch Queue
	var queue domain.Queue
	if err := s.queueCol.FindOne(ctx, bson.M{"_id": queueID}).Decode(&queue); err != nil {
		return nil, err
	}

	// 2. Fetch All Entries
	cursor, err := s.entryCol.Find(ctx, bson.M{"queue_id": queueID}, options.Find().SetSort(bson.M{"created_at": 1}))
	if err != nil {
		return nil, err
	}
	var entries []domain.Entry
	if err := cursor.All(ctx, &entries); err != nil {
		return nil, err
	}

	// 3. Stats Calculation
	totalBookings := len(entries)
	servedCount := 0
	skippedCount := 0
	var totalWait time.Duration
	hourlyCounts := make(map[int]int)

	historyEntries := make([]dto.HistoryEntry, len(entries))
	for i, e := range entries {
		historyEntries[i] = dto.HistoryEntry{
			TicketNo:    e.TicketNo,
			DisplayName: e.Name,
			Email:       e.Email,
			Phone:       e.Phone,
			Status:      e.Status,
		}

		hourlyCounts[e.CreatedAt.Hour()]++

		switch e.Status {
		case constants.EntryStatusServed:
			servedCount++
			if e.ServedAt != nil {
				totalWait += e.ServedAt.Sub(e.CreatedAt)
				historyEntries[i].ServedAt = e.ServedAt.Format(time.RFC3339)
				historyEntries[i].WaitTimeMin = int(e.ServedAt.Sub(e.CreatedAt).Minutes())
			}
		case constants.EntryStatusSkipped, constants.EntryStatusLeft:
			skippedCount++
		}
	}

	avgWait := "0m"
	if servedCount > 0 {
		avgWait = fmt.Sprintf("%dm", int((totalWait / time.Duration(servedCount)).Minutes()))
	}

	// 4. Peak Volume Calculation
	peakHour := -1
	maxCount := 0
	for h, c := range hourlyCounts {
		if c > maxCount {
			maxCount = c
			peakHour = h
		}
	}
	peakVolume := "N/A"
	if peakHour != -1 {
		peakVolume = fmt.Sprintf("%02d:00 - %02d:00", peakHour, peakHour+1)
	}

	// 5. Timeline Interleaving
	type rawEvent struct {
		Timestamp time.Time
		Type      string
		Value     string
		Metadata  interface{}
	}
	var events []rawEvent

	// Add Queue activities
	for _, logEntry := range queue.ActivityLogs {
		events = append(events, rawEvent{
			Timestamp: logEntry.Timestamp,
			Type:      "STATUS_CHANGE",
			Value:     logEntry.Value,
		})
	}

	// Add Entry activities
	for _, e := range entries {
		events = append(events, rawEvent{Timestamp: e.CreatedAt, Type: "JOINED", Metadata: e})
		if e.ServedAt != nil {
			events = append(events, rawEvent{Timestamp: *e.ServedAt, Type: "SERVED", Metadata: e})
		} else if e.FinishedAt != nil && e.Status != constants.EntryStatusServed {
			events = append(events, rawEvent{Timestamp: *e.FinishedAt, Type: "SKIPPED", Metadata: e})
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// 6. Timeline Aggregation (5 min window for JOINED)
	var finalTimeline []dto.TimelineEvent
	for i := 0; i < len(events); i++ {
		ev := events[i]

		if ev.Type == "JOINED" {
			windowEnd := ev.Timestamp.Add(5 * time.Minute)
			var subEvents []dto.TimelineSubEvent
			count := 0

			j := i
			for j < len(events) && events[j].Type == "JOINED" && events[j].Timestamp.Before(windowEnd) {
				sub := events[j].Metadata.(domain.Entry)
				subEvents = append(subEvents, dto.TimelineSubEvent{
					TicketNo: sub.TicketNo,
					Name:     sub.Name,
					Action:   "Joined",
					Time:     sub.CreatedAt.Format("15:04"),
				})
				count++
				j++
			}

			msg := "1 guest joined the queue"
			if count > 1 {
				msg = fmt.Sprintf("%d guests joined the queue", count)
			}

			finalTimeline = append(finalTimeline, dto.TimelineEvent{
				Type:      "JOINED",
				Timestamp: ev.Timestamp.Format("15:04"),
				Message:   msg,
				Color:     "plum-soft",
				SubEvents: subEvents,
			})
			i = j - 1
		} else {
			tEvent := dto.TimelineEvent{
				Type:      ev.Type,
				Timestamp: ev.Timestamp.Format("15:04"),
			}

			switch ev.Type {
			case "STATUS_CHANGE":
				tEvent.Message = fmt.Sprintf("Queue status changed to %s", ev.Value)
				tEvent.Color = "plum-muted"

				switch ev.Value {
				case "CREATED", "ACTIVE":
					tEvent.Color = "mint"
				case "PAUSED":
					tEvent.Color = "warning"
				case "EXPIRED":
					tEvent.Color = "danger"
				}
			case "SERVED":
				sub := ev.Metadata.(domain.Entry)
				tEvent.Message = fmt.Sprintf("Guest %s (%s) was served", sub.Name, sub.TicketNo)
				tEvent.Color = "mint"
			case "SKIPPED":
				sub := ev.Metadata.(domain.Entry)
				tEvent.Message = fmt.Sprintf("Guest %s (%s) left or was skipped", sub.Name, sub.TicketNo)
				tEvent.Color = "danger"
			}

			finalTimeline = append(finalTimeline, tEvent)
		}
	}

	// 7. Simple Insights
	insights := []string{
		fmt.Sprintf("You served %d guests in this session.", servedCount),
	}
	if servedCount > 0 && totalBookings > 0 {
		conversion := (float64(servedCount) / float64(totalBookings)) * 100
		insights = append(insights, fmt.Sprintf("Session conversion rate: %.1f%%", conversion))
	}
	if peakVolume != "N/A" {
		insights = append(insights, fmt.Sprintf("Peak activity was detected around %s.", peakVolume))
	}

	response := &dto.HistoryDetailResponse{
		QueueName: queue.Name,
		Date:      queue.CreatedAt.Format("02 Jan 2006"),
		Status:    queue.Status,
		Notes:     queue.Notes,
		Stats: dto.SessionStats{
			TotalBookings: totalBookings,
			TotalServed:   servedCount,
			TotalSkipped:  skippedCount,
			AvgWaitTime:   avgWait,
			PeakVolume:    peakVolume,
		},
		Insights: insights,
		Timeline: finalTimeline,
		Entries:  historyEntries,
	}

	if queue.ClosedAt != nil {
		cAt := queue.ClosedAt.Format(time.RFC3339)
		response.ClosedAt = &cAt
	}

	return response, nil
}

func (s *Service) DeleteQueue(ctx context.Context, hostID string, queueID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var queue domain.Queue
	if err := s.queueCol.FindOne(ctx, bson.M{"_id": queueID, "host_id": hostID}).Decode(&queue); err != nil {
		return err
	}

	_, err := s.entryCol.DeleteMany(ctx, bson.M{"queue_id": queueID})
	if err != nil {
		return err
	}

	_, err = s.queueCol.DeleteOne(ctx, bson.M{"_id": queueID})
	if err != nil {
		return err
	}

	if queue.Status == constants.QueueStatusActive || queue.Status == constants.QueueStatusPaused {
		if queue.JoinCode != "" {
			_ = s.redisRepo.ReleaseJoinCode(ctx, queue.JoinCode)
		}
	}
	_ = s.billingSvc.DecrementMonthlyQueueCount(ctx, hostID, queue.CreatedAt)

	if queue.HostPublicID != nil {
		_ = s.redisRepo.InvalidateHistorySummary(ctx, *queue.HostPublicID)
		s.InvalidateDashboardCache(*queue.HostPublicID)
	}

	return nil
}

func (s *Service) DeleteQueuesBulk(ctx context.Context, hostID string, queueIDs []string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if len(queueIDs) == 0 {
		return nil
	}

	cursor, err := s.queueCol.Find(ctx, bson.M{
		"_id":     bson.M{"$in": queueIDs},
		"host_id": hostID,
	})
	if err != nil {
		return err
	}
	var queues []domain.Queue
	if err := cursor.All(ctx, &queues); err != nil {
		return err
	}

	validIDs := make([]string, len(queues))
	hostPublicIDs := make(map[string]struct{})
	for i, q := range queues {
		validIDs[i] = q.ID
		if q.HostPublicID != nil {
			hostPublicIDs[*q.HostPublicID] = struct{}{}
		}
	}

	if len(validIDs) == 0 {
		return nil
	}

	_, err = s.entryCol.DeleteMany(ctx, bson.M{"queue_id": bson.M{"$in": validIDs}})
	if err != nil {
		return err
	}

	_, err = s.queueCol.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": validIDs}})
	if err != nil {
		return err
	}

	decrCreatedAts := make([]time.Time, 0)
	activeIDs := make([]string, 0)
	activeCodes := make([]string, 0)

	for _, q := range queues {
		decrCreatedAts = append(decrCreatedAts, q.CreatedAt)
		if q.Status == constants.QueueStatusActive || q.Status == constants.QueueStatusPaused {
			activeIDs = append(activeIDs, q.ID)
			if q.JoinCode != "" {
				activeCodes = append(activeCodes, q.JoinCode)
			}
		}
	}

	if len(activeIDs) > 0 {
		_ = s.redisRepo.DeleteQueuesKeysBulk(ctx, activeIDs)
	}
	if len(activeCodes) > 0 {
		_ = s.redisRepo.ReleaseJoinCodesBulk(ctx, activeCodes)
	}

	if len(decrCreatedAts) > 0 {
		_ = s.billingSvc.DecrementMonthlyQueueCountBulk(ctx, hostID, decrCreatedAts)
	}

	for pid := range hostPublicIDs {
		_ = s.redisRepo.InvalidateHistorySummary(ctx, pid)
		s.InvalidateDashboardCache(pid)
	}

	return nil
}

func (s *Service) IsQueueCapacityExceeded(ctx context.Context, queueID string) (bool, error) {
	queue, err := s.GetQueue(ctx, queueID)
	if err != nil {
		return false, err
	}

	hostID := ""
	if queue.HostID != nil {
		hostID = *queue.HostID
	}

	currentCount, _ := s.GetWaitingCount(ctx, queueID)
	return s.billingSvc.IsLimitExceeded(ctx, hostID, "guests", int(currentCount+1))
}
