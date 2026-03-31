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
	"golang.org/x/crypto/bcrypt"
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

	// 3. PIN Hashing
	var pinHash *string
	if params.PIN != nil && *params.PIN != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*params.PIN), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash PIN: %w", err)
		}
		h := string(hash)
		pinHash = &h
	}

	// 4. Delegate Creation
	entry := queuedomain.Entry{
		QueueID:   params.QueueID,
		Name:      params.Name,
		Email:     params.Email,
		Phone:     params.Phone,
		PartySize: params.PartySize,
		FCMToken:  params.FCMToken,
		PINHash:   pinHash,
		CreatedBy: params.CreatedBy,
	}

	result, err := s.queueService.CreateEntry(ctx, entry)
	if err != nil {
		return nil, err
	}

	// 5. Customer Session Management
	if err := s.redisRepo.SetUserSession(ctx, entry.QueueID, result.Entry.ID, 24*time.Hour); err != nil {
		return nil, fmt.Errorf("failed to set user session: %w", err)
	}

	return result, nil
}

func (s *Service) SetUserEmail(ctx context.Context, entryID, email string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.entryCol.UpdateOne(ctx, bson.M{"_id": entryID}, bson.M{"$set": bson.M{"email": email}})
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
	if err != nil || !exists {
		return nil, fmt.Errorf("session expired or invalid")
	}
	if entry.Status == constants.EntryStatusServed || entry.Status == constants.EntryStatusLeft || entry.Status == constants.EntryStatusSkipped {
		return nil, fmt.Errorf("your queue session has ended")
	}
	position, _ := s.queueService.GetPosition(ctx, entry.QueueID, entryID)
	return &queueservice.JoinQueueResult{
		Entry:    *entry,
		Position: position + 1,
	}, nil
}

func (s *Service) RejoinByPIN(ctx context.Context, queueID, ticketNo, pin string) (*queueservice.JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var entry queuedomain.Entry
	if err := s.entryCol.FindOne(ctx, bson.M{"queue_id": queueID, "ticket_no": ticketNo}).Decode(&entry); err != nil {
		return nil, fmt.Errorf("entry not found")
	}
	if entry.PINHash == nil {
		return nil, fmt.Errorf("no PIN set for this ticket")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*entry.PINHash), []byte(pin)); err != nil {
		return nil, fmt.Errorf("invalid PIN")
	}
	position, _ := s.queueService.GetPosition(ctx, queueID, entry.ID)
	return &queueservice.JoinQueueResult{
		Entry:    entry,
		Position: position + 1,
	}, nil
}

func (s *Service) GetEntry(ctx context.Context, entryID string) (*queuedomain.Entry, error) {
	return s.queueService.GetEntry(ctx, entryID)
}

func (s *Service) SessionExists(ctx context.Context, queueID, entryID string) (bool, error) {
	return s.redisRepo.UserSessionExists(ctx, queueID, entryID)
}

func (s *Service) Leave(ctx context.Context, entryID string) error {
	return s.queueService.RemoveUser(ctx, entryID)
}

func (s *Service) Heartbeat(ctx context.Context, queueID, entryID string) error {
	ttl := time.Duration(constants.HeartbeatTTLSec) * time.Second
	return s.redisRepo.SetHeartbeat(ctx, queueID, entryID, ttl)
}

func (s *Service) MarkArrived(ctx context.Context, queueID, entryID string) error {
	// 1. Update status in database
	_, err := s.entryCol.UpdateOne(ctx,
		bson.M{"_id": entryID, "queue_id": queueID},
		bson.M{"$set": bson.M{"status": constants.EntryStatusArrived}},
	)
	if err != nil {
		return err
	}

	// 2. Remove heartbeat so expiry service doesn't trigger idle/grace transitions
	_ = s.redisRepo.SetHeartbeat(ctx, queueID, entryID, 24*time.Hour) // Keep active but long TTL
	return nil
}
