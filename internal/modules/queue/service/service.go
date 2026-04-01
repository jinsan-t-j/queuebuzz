package service

import (
	"context"
	"fmt"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/modules/queue/domain"

	"queuebuzz/internal/modules/queue/repository"

	"github.com/google/uuid"
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
	return s.updateQueueStatus(ctx, queueID, constants.QueueStatusClosed)
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
	_, err := s.queueCol.UpdateOne(ctx, bson.M{"_id": queueID}, bson.M{"$set": bson.M{"status": status}})
	return err
}

func (s *Service) CreateEntry(ctx context.Context, entry domain.Entry) (*JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

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

	position, err := s.redisRepo.GetPosition(ctx, entry.QueueID, entry.ID)
	if err != nil {
		position = 0
	}

	return &JoinQueueResult{
		Entry:    entry,
		Position: position + 1,
	}, nil
}

func (s *Service) GetQueueEntries(ctx context.Context, queueID string) ([]domain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cursor, err := s.entryCol.Find(ctx, bson.M{
		"queue_id": queueID,
		"status":   bson.M{"$in": []string{constants.EntryStatusWaiting, constants.EntryStatusCalled, constants.EntryStatusIdle}},
	}, options.Find().SetSort(bson.M{"created_at": 1}))
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
	if err != nil || len(entryIDs) == 0 {
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

	position, err := s.redisRepo.GetPosition(ctx, entry.QueueID, entry.ID)
	if err != nil {
		position = 0
	}

	return &JoinQueueResult{
		Entry:    entry,
		Position: position + 1,
	}, nil
}

func (s *Service) GetPosition(ctx context.Context, queueID, entryID string) (int64, error) {
	return s.redisRepo.GetPosition(ctx, queueID, entryID)
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
