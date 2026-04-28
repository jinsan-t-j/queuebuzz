package service

import (
	"context"
	"fmt"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/exceptions"
	customerrepo "queuebuzz/internal/modules/customer/repository"
	queuedomain "queuebuzz/internal/modules/queue/domain"
	queueservice "queuebuzz/internal/modules/queue/service"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Service struct {
	entryCol     *mongodriver.Collection
	redisRepo    *customerrepo.RedisRepository
	queueService *queueservice.Service
}

func New(
	entryCol *mongodriver.Collection,
	redisRepo *customerrepo.RedisRepository,
	queueService *queueservice.Service,
) *Service {
	return &Service{
		entryCol:     entryCol,
		redisRepo:    redisRepo,
		queueService: queueService,
	}
}

func (s *Service) GetEntryStatusByID(ctx context.Context, entryID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var result struct {
		Status string `bson:"status"`
	}

	err := s.entryCol.FindOne(
		ctx,
		bson.M{"_id": entryID},
		options.FindOne().SetProjection(bson.M{"status": 1}),
	).Decode(&result)

	if err != nil {
		return "", err
	}

	return result.Status, nil
}

func (s *Service) JoinQueue(ctx context.Context, params queueservice.JoinQueueParams) (*queueservice.JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 1. Validate Queue Status
	queue, err := s.queueService.GetQueue(ctx, params.QueueID)
	if err != nil {
		return nil, fmt.Errorf("queue not found")
	}
	if queue.Status != constants.QueueStatusActive && queue.Status != constants.QueueStatusPaused {
		return nil, fmt.Errorf("queue is not active")
	}
	if time.Now().After(queue.ExpiresAt) {
		return nil, fmt.Errorf("queue has expired")
	}

	// 2. Duplicate Check
	if params.Email != nil && *params.Email != "" {
		count, _ := s.entryCol.CountDocuments(ctx, bson.M{
			"queue_id": params.QueueID,
			"status":   bson.M{"$in": []string{constants.EntryStatusWaiting, constants.EntryStatusCalled, constants.EntryStatusIdle}},
			"email":    *params.Email,
		})
		if count > 0 {
			return nil, exceptions.Duplicate("email", "Guest already in queue!")
		}
	}

	// 4. Delegate Creation
	entry := queuedomain.Entry{
		QueueID:     params.QueueID,
		Name:        params.Name,
		Email:       params.Email,
		Phone:       params.Phone,
		PartySize:   params.PartySize,
		FCMToken:    params.FCMToken,
		CreatedBy:   params.CreatedBy,
		Fingerprint: params.Fingerprint,
		Metadata:    params.Metadata,
	}

	result, err := s.queueService.CreateEntry(ctx, entry)
	if err != nil {
		return nil, err
	}

	// 5. Customer Session Management
	if err := s.redisRepo.SetUserSession(ctx, entry.QueueID, result.ID, 24*time.Hour); err != nil {
		return nil, fmt.Errorf("failed to set user session: %w", err)
	}

	return result, nil
}

func (s *Service) UpdateEntry(ctx context.Context, entryID string, updates bson.M) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.entryCol.UpdateOne(ctx, bson.M{"_id": entryID}, bson.M{"$set": updates})
	return err
}

func (s *Service) RejoinByID(ctx context.Context, entryID string) (*queueservice.JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	entry, err := s.queueService.GetEntry(ctx, entryID)
	if err != nil {
		return nil, fmt.Errorf("entry not found")
	}

	exists, err := s.redisRepo.UserSessionExists(ctx, entry.QueueID, entryID)
	if err != nil {
		return nil, fmt.Errorf("failed to check session status")
	}

	if !exists {
		// Resilience: Fallback to MongoDB if Redis session is missing.
		// Since the JWT was already verified by the handler/middleware,
		// we just need to ensure the entry is still active in the database.
		isTerminal := (entry.Status == constants.EntryStatusServed ||
			entry.Status == constants.EntryStatusLeft ||
			entry.Status == constants.EntryStatusSkipped)

		if isTerminal {
			return nil, fmt.Errorf("your queue session has ended")
		}

		// Re-Sync: User holds a valid JWT and entry is active in DB.
		_ = s.redisRepo.SetUserSession(ctx, entry.QueueID, entryID, 24*time.Hour)
	} else {
		// Even if in Redis, double-check terminal statuses from DB snapshot
		if entry.Status == constants.EntryStatusServed || entry.Status == constants.EntryStatusLeft || entry.Status == constants.EntryStatusSkipped {
			_ = s.redisRepo.DeleteUserSession(ctx, entry.QueueID, entryID)
			return nil, fmt.Errorf("your queue session has ended")
		}
	}

	position, _ := s.queueService.GetPosition(ctx, entry.QueueID, entryID)
	return &queueservice.JoinQueueResult{
		Entry:    *entry,
		Position: position + 1,
	}, nil
}

func (s *Service) GetEntry(ctx context.Context, entryID string) (*queuedomain.Entry, error) {
	return s.queueService.GetEntry(ctx, entryID)
}

func (s *Service) SessionExists(ctx context.Context, queueID, entryID string) (bool, error) {
	exists, err := s.redisRepo.UserSessionExists(ctx, queueID, entryID)
	if err == nil && exists {
		return true, nil
	}

	// Resilience: If Redis session missing, check DB
	entry, err := s.queueService.GetEntry(ctx, entryID)
	if err != nil || entry == nil || entry.QueueID != queueID {
		return false, nil
	}

	isTerminal := (entry.Status == constants.EntryStatusServed ||
		entry.Status == constants.EntryStatusLeft ||
		entry.Status == constants.EntryStatusSkipped)

	if !isTerminal {
		// Re-sync Redis session
		_ = s.redisRepo.SetUserSession(ctx, queueID, entryID, 24*time.Hour)
		return true, nil
	}

	return false, nil
}

func (s *Service) Leave(ctx context.Context, entryID string) error {
	return s.queueService.RemoveUser(ctx, entryID)
}

func (s *Service) MarkArrived(ctx context.Context, queueID, entryID string) error {
	_, err := s.entryCol.UpdateOne(ctx,
		bson.M{"_id": entryID, "queue_id": queueID},
		bson.M{"$set": bson.M{"status": constants.EntryStatusArrived, "updated_at": time.Now()}},
	)
	if err != nil {
		return err
	}

	// Keep session active with long TTL
	_ = s.redisRepo.SetUserSession(ctx, queueID, entryID, 24*time.Hour)

	// Clear any pending expiry timers
	_ = s.redisRepo.ClearIdleTimer(ctx, queueID, entryID)
	_ = s.redisRepo.ClearGraceTimer(ctx, queueID, entryID)

	return nil
}

func (s *Service) ConfirmStillHere(ctx context.Context, queueID, entryID string) error {
	_, err := s.entryCol.UpdateOne(ctx,
		bson.M{"_id": entryID, "queue_id": queueID},
		bson.M{"$set": bson.M{"status": constants.EntryStatusWaiting, "updated_at": time.Now()}},
	)
	if err != nil {
		return err
	}

	// Clear the grace timer so they doesn't get skipped automatically
	_ = s.redisRepo.ClearGraceTimer(ctx, queueID, entryID)

	return nil
}
