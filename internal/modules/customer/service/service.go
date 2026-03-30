package service

import (
	"context"
	"fmt"
	"time"

	"queuebuzz/internal/constants"
	queuedomain "queuebuzz/internal/modules/queue/domain"
	queueservice "queuebuzz/internal/modules/queue/service"
	legacyservices "queuebuzz/internal/services"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	entryCol     *mongodriver.Collection
	redisService *legacyservices.RedisService
	queueService *queueservice.Service
}

func New(
	entryCol *mongodriver.Collection,
	redisSvc *legacyservices.RedisService,
	queueSvc *queueservice.Service,
) *Service {
	return &Service{
		entryCol:     entryCol,
		redisService: redisSvc,
		queueService: queueSvc,
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

func (s *Service) SetUserEmail(ctx context.Context, entryID, email string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.entryCol.UpdateOne(ctx, bson.M{"_id": entryID}, bson.M{"$set": bson.M{"email": email}})
	return err
}

func (s *Service) SetUserPIN(ctx context.Context, entryID, pin string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash PIN: %w", err)
	}
	_, err = s.entryCol.UpdateOne(ctx, bson.M{"_id": entryID}, bson.M{"$set": bson.M{"pin_hash": string(hash)}})
	return err
}

func (s *Service) RejoinByID(ctx context.Context, entryID string) (*queueservice.JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	entry, err := s.queueService.GetEntry(ctx, entryID)
	if err != nil {
		return nil, fmt.Errorf("entry not found")
	}
	exists, err := s.redisService.UserSessionExists(ctx, entry.QueueID, entryID)
	if err != nil || !exists {
		return nil, fmt.Errorf("session expired or invalid")
	}
	if entry.Status == constants.EntryStatusServed || entry.Status == constants.EntryStatusLeft || entry.Status == constants.EntryStatusSkipped {
		return nil, fmt.Errorf("your queue session has ended")
	}
	position, _ := s.redisService.GetPosition(ctx, entry.QueueID, entryID)
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
	position, _ := s.redisService.GetPosition(ctx, queueID, entry.ID)
	return &queueservice.JoinQueueResult{
		Entry:    entry,
		Position: position + 1,
	}, nil
}

func (s *Service) GetEntry(ctx context.Context, entryID string) (*queuedomain.Entry, error) {
	return s.queueService.GetEntry(ctx, entryID)
}
