package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/log"
	"queuebuzz/internal/modules/queue/domain"

	"queuebuzz/internal/modules/queue/repository"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Service struct {
	queueCol  *mongodriver.Collection
	entryCol  *mongodriver.Collection
	redisRepo *repository.RedisRepository
}

func New(
	queueCol *mongodriver.Collection,
	entryCol *mongodriver.Collection,
	redisRepo *repository.RedisRepository,
) *Service {
	return &Service{
		queueCol:  queueCol,
		entryCol:  entryCol,
		redisRepo: redisRepo,
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
	RecoveryEmail     *string
}

type JoinQueueParams struct {
	QueueID   string
	Name      string
	Email     *string
	Phone     *string
	PartySize *int
	FCMToken  *string
	CreatedBy *string
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

	now := time.Now()
	queue := domain.Queue{
		ID:                uuid.New().String(),
		HostID:            params.HostID,
		HostPublicID:      params.HostPublicID,
		Name:              params.Name,
		Slug:              params.Slug,
		AvgServiceMins:    params.AvgServiceMins,
		AllowPartyJoining: *params.AllowPartyJoining,
		MaxPartySize:      *params.MaxPartySize,
		Status:            constants.QueueStatusActive,
		CreatedAt:         now,
		UpdatedAt:         now,
		ExpiresAt:         now.Add(time.Duration(constants.DefaultQueueExpiryH) * time.Hour),
	}

	joinCode, err := s.redisRepo.GenerateJoinCode(ctx, queue.ID)
	if err != nil {
		return nil, err
	}

	queue.JoinCode = joinCode
	if _, err := s.queueCol.InsertOne(ctx, queue); err != nil {
		return nil, fmt.Errorf("failed to create queue: %w", err)
	}

	// Warm up Redis re-hydration sentinel (avoid redundant first-load query to DB)
	_ = s.redisRepo.SetRehydratedSentinel(ctx, queue.ID, 1*time.Minute)

	// Invalidate history summary cache
	if params.HostPublicID != nil {
		_ = s.redisRepo.InvalidateHistorySummary(ctx, *params.HostPublicID)
	}

	return &queue, nil
}

func (s *Service) CheckSlugAvailability(ctx context.Context, slug string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	count, err := s.queueCol.CountDocuments(ctx, bson.M{"slug": slug})
	if err != nil {
		return false, err
	}
	return count == 0, nil
}

func (s *Service) GetQueue(ctx context.Context, queueID string) (*domain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var queue domain.Queue
	if err := s.queueCol.FindOne(ctx, bson.M{"_id": queueID}).Decode(&queue); err != nil {
		return nil, err
	}
	return &queue, nil
}

func (s *Service) TerminateQueue(ctx context.Context, queueID string) error {
	err := s.updateQueueStatus(ctx, queueID, constants.QueueStatusClosed)
	if err == nil {
		s.invalidateHostSummaryByQueueID(ctx, queueID)
	}
	return err
}

func (s *Service) invalidateHostSummaryByQueueID(ctx context.Context, queueID string) {
	queue, err := s.GetQueue(ctx, queueID)
	if err == nil && queue != nil && queue.HostPublicID != nil {
		_ = s.redisRepo.InvalidateHistorySummary(ctx, *queue.HostPublicID)
	}
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
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err := s.queueCol.FindOneAndUpdate(ctx, bson.M{"_id": queueID}, bson.M{"$set": updates}, opts).Decode(&queue)
	if err != nil {
		return nil, err
	}
	return &queue, nil
}

func (s *Service) updateQueueStatus(ctx context.Context, queueID, status string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.queueCol.UpdateOne(ctx, bson.M{"_id": queueID}, bson.M{"$set": bson.M{"status": status, "updated_at": time.Now()}})
	return err
}

func (s *Service) CreateEntry(ctx context.Context, entry domain.Entry) (*JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Ensure ticket counter is initialized (fallback to DB if Redis wiped)
	if err := s.ensureTicketCounter(ctx, entry.QueueID); err != nil {
		return nil, err
	}

	ticketNo, err := s.redisRepo.NextTicket(ctx, entry.QueueID)
	if err != nil {
		return nil, err
	}
	entry.TicketNo = ticketNo

	now := time.Now()
	entry.ID = uuid.New().String()
	entry.Token = uuid.New().String()
	entry.Status = constants.EntryStatusWaiting
	entry.CreatedAt = now
	entry.UpdatedAt = now

	if _, err := s.entryCol.InsertOne(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to insert queue entry: %w", err)
	}

	score := float64(now.Unix())
	if err := s.redisRepo.AddToQueue(ctx, entry.QueueID, entry.ID, score); err != nil {
		return nil, fmt.Errorf("failed to add to queue positions: %w", err)
	}

	position, err := s.GetPosition(ctx, entry.QueueID, entry.ID)
	if err != nil {
		position = 0
	}

	return &JoinQueueResult{
		Entry:    entry,
		Position: position + 1,
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
	return nil
}

func (s *Service) GetEntryStatusByID(ctx context.Context, entryID string) (*JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var entry domain.Entry
	if err := s.entryCol.FindOne(ctx, bson.M{"_id": entryID}).Decode(&entry); err != nil {
		return nil, err
	}

	position, err := s.GetPosition(ctx, entry.QueueID, entry.ID)
	if err != nil {
		position = 0
	}

	if position == -1 {
		position = 0
	}

	return &JoinQueueResult{
		Entry:    entry,
		Position: position + 1,
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

type QueueWithStats struct {
	domain.Queue `bson:",inline"`
	TotalServed  int   `bson:"total_served"`
	AvgWaitMS    int64 `bson:"avg_wait_ms"`
}

func (s *Service) GetQueueHistoryListForHost(ctx context.Context, hostPublicID string, search, status string, page, limit int) ([]QueueWithStats, int64, error) {
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
					"from": "entries",
					"let":  bson.M{"queue_id": "$_id"},
					"pipeline": bson.A{
						bson.M{"$match": bson.M{
							"$expr":  bson.M{"$eq": []string{"$queue_id", "$$queue_id"}},
							"status": constants.EntryStatusServed,
						}},
					},
					"as": "served_entries",
				}},
				bson.M{"$addFields": bson.M{
					"total_served": bson.M{"$size": "$served_entries"},
					"avg_wait_ms": bson.M{
						"$cond": bson.A{
							bson.M{"$gt": []interface{}{bson.M{"$size": "$served_entries"}, 0}},
							bson.M{"$avg": bson.M{"$subtract": []string{"$served_entries.served_at", "$served_entries.created_at"}}},
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

func (s *Service) GetHostHistorySummary(ctx context.Context, hostPublicID string) (*domain.HostHistorySummary, error) {
	// 1. Try cache
	if summary, err := s.redisRepo.GetHistorySummary(ctx, hostPublicID); err == nil && summary != nil {
		return summary, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pipeline := mongodriver.Pipeline{
		// 1. Match queues for this host that are not active/paused
		{{Key: "$match", Value: bson.M{
			"host_public_id": hostPublicID,
			"status":         bson.M{"$nin": []string{constants.QueueStatusActive, constants.QueueStatusPaused}},
		}}},
		// 2. Lookup served entries
		{{Key: "$lookup", Value: bson.M{
			"from": "entries",
			"let":  bson.M{"queue_id": "$_id"},
			"pipeline": bson.A{
				bson.M{"$match": bson.M{
					"$expr":  bson.M{"$eq": []string{"$queue_id", "$$queue_id"}},
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
						"in":    bson.M{"$subtract": []string{"$$e.served_at", "$$e.created_at"}},
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
	if err := s.queueCol.FindOne(ctx, bson.M{
		"_id":        queueID,
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

func (s *Service) GetQueueByJoinCode(ctx context.Context, joinCode string) (*domain.Queue, error) {
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

func (s *Service) GetQueueHistory(ctx context.Context, queueID string) ([]domain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cursor, err := s.entryCol.Find(ctx, bson.M{
		"queue_id": queueID,
		"status":   bson.M{"$in": []string{constants.EntryStatusServed, constants.EntryStatusSkipped, constants.EntryStatusLeft}},
	}, options.Find().SetSort(bson.M{"joined_at": -1}))
	if err != nil {
		return nil, err
	}

	var entries []domain.Entry
	if err := cursor.All(ctx, &entries); err != nil {
		return nil, err
	}
	return entries, nil
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
		// Parse Q-0042 -> 42
		fmt.Sscanf(lastEntry.TicketNo, "Q-%04d", &lastNum)
	}

	// Sync Redis with DB last known ticket
	log.Info().Str("queue_id", queueID).Int64("last_num", lastNum).Msg("Redis: re-hydrated ticket counter from MongoDB")
	return s.redisRepo.SetTicketCounter(ctx, queueID, lastNum)
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
			Score:  float64(entry.CreatedAt.Unix()),
			Member: entry.ID,
		}
	}

	_ = s.redisRepo.AddToQueueBulk(ctx, queueID, members)

	return ids, nil
}
