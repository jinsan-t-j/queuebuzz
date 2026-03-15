package service

import (
	"context"
	"fmt"
	"time"

	"queuebuzz/internal/constants"
	queuedomain "queuebuzz/internal/modules/queue/domain"
	legacyservices "queuebuzz/internal/services"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	queueCol      *mongodriver.Collection
	entryCol      *mongodriver.Collection
	redisService  *legacyservices.RedisService
	ticketService *legacyservices.TicketService
	joinCodeSvc   *legacyservices.JoinCodeService
	geoService    *legacyservices.GeoService
}

func New(
	queueCol *mongodriver.Collection,
	entryCol *mongodriver.Collection,
	redisSvc *legacyservices.RedisService,
	ticketSvc *legacyservices.TicketService,
	joinCodeSvc *legacyservices.JoinCodeService,
	geoSvc *legacyservices.GeoService,
) *Service {
	return &Service{
		queueCol:      queueCol,
		entryCol:      entryCol,
		redisService:  redisSvc,
		ticketService: ticketSvc,
		joinCodeSvc:   joinCodeSvc,
		geoService:    geoSvc,
	}
}

type CreateQueueParams struct {
	HostID         *string
	HostPublicID   *string
	HostLat        float64
	HostLng        float64
	RadiusM        int
	AvgServiceMins int
}

type JoinQueueParams struct {
	QueueID     string
	Lat         float64
	Lng         float64
	FCMToken    string
	DisplayName *string
	PIN         *string
}

type JoinQueueResult struct {
	Token            string `json:"token"`
	TicketNo         string `json:"ticket_no"`
	Position         int64  `json:"position"`
	EstimatedWaitMin int64  `json:"estimated_wait_mins"`
}

func (s *Service) CreateQueue(ctx context.Context, params CreateQueueParams) (*queuedomain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if params.RadiusM <= 0 {
		params.RadiusM = constants.DefaultRadiusM
	}
	if params.AvgServiceMins <= 0 {
		params.AvgServiceMins = constants.DefaultAvgServiceMins
	}

	joinCode, err := s.joinCodeSvc.GenerateJoinCode(ctx, "")
	if err != nil {
		return nil, err
	}

	now := time.Now()
	queue := queuedomain.Queue{
		ID:             uuid.New().String(),
		HostID:         params.HostID,
		HostPublicID:   params.HostPublicID,
		HostLat:        params.HostLat,
		HostLng:        params.HostLng,
		RadiusM:        params.RadiusM,
		JoinCode:       joinCode,
		Status:         constants.QueueStatusActive,
		CreatedAt:      now,
		ExpiresAt:      now.Add(time.Duration(constants.DefaultQueueExpiryH) * time.Hour),
		AvgServiceMins: params.AvgServiceMins,
	}

	_ = s.joinCodeSvc.DeleteJoinCode(ctx, joinCode)
	joinCode, err = s.joinCodeSvc.GenerateJoinCode(ctx, queue.ID)
	if err != nil {
		return nil, err
	}
	queue.JoinCode = joinCode

	if _, err := s.queueCol.InsertOne(ctx, queue); err != nil {
		return nil, fmt.Errorf("failed to create queue: %w", err)
	}
	return &queue, nil
}

func (s *Service) GetQueue(ctx context.Context, queueID string) (*queuedomain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var queue queuedomain.Queue
	if err := s.queueCol.FindOne(ctx, bson.M{"_id": queueID}).Decode(&queue); err != nil {
		return nil, err
	}
	return &queue, nil
}

func (s *Service) CloseQueue(ctx context.Context, queueID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.queueCol.UpdateOne(ctx, bson.M{"_id": queueID}, bson.M{"$set": bson.M{"status": constants.QueueStatusClosed}})
	return err
}

func (s *Service) JoinQueue(ctx context.Context, params JoinQueueParams) (*JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	queue, err := s.GetQueue(ctx, params.QueueID)
	if err != nil {
		return nil, fmt.Errorf("queue not found")
	}
	if queue.Status != constants.QueueStatusActive {
		return nil, fmt.Errorf("queue is not active")
	}
	if time.Now().After(queue.ExpiresAt) {
		return nil, fmt.Errorf("queue has expired")
	}
	if !s.geoService.IsWithinRadius(queue.HostLat, queue.HostLng, params.Lat, params.Lng, queue.RadiusM) {
		return nil, fmt.Errorf("you are too far from the queue location")
	}

	userToken := uuid.New().String()
	ticketNo, err := s.ticketService.NextTicket(ctx, params.QueueID)
	if err != nil {
		return nil, err
	}

	var pinHash *string
	if params.PIN != nil && *params.PIN != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(*params.PIN), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash PIN: %w", err)
		}
		h := string(hash)
		pinHash = &h
	}

	entry := queuedomain.Entry{
		Token:       userToken,
		QueueID:     params.QueueID,
		TicketNo:    ticketNo,
		Status:      constants.EntryStatusWaiting,
		JoinedAt:    time.Now(),
		FCMToken:    params.FCMToken,
		DisplayName: params.DisplayName,
		PINHash:     pinHash,
	}
	if _, err := s.entryCol.InsertOne(ctx, entry); err != nil {
		return nil, fmt.Errorf("failed to insert queue entry: %w", err)
	}

	score := float64(time.Now().Unix())
	if err := s.redisService.AddToQueue(ctx, params.QueueID, userToken, score); err != nil {
		return nil, fmt.Errorf("failed to add to queue positions: %w", err)
	}
	if err := s.redisService.SetUserSession(ctx, params.QueueID, userToken, 24*time.Hour); err != nil {
		return nil, fmt.Errorf("failed to set user session: %w", err)
	}

	position, err := s.redisService.GetPosition(ctx, params.QueueID, userToken)
	if err != nil {
		position = 0
	}

	return &JoinQueueResult{
		Token:            userToken,
		TicketNo:         ticketNo,
		Position:         position + 1,
		EstimatedWaitMin: position * int64(queue.AvgServiceMins),
	}, nil
}

func (s *Service) GetQueueEntries(ctx context.Context, queueID string) ([]queuedomain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cursor, err := s.entryCol.Find(ctx, bson.M{
		"queue_id": queueID,
		"status":   bson.M{"$in": []string{constants.EntryStatusWaiting, constants.EntryStatusCalled, constants.EntryStatusIdle}},
	})
	if err != nil {
		return nil, err
	}

	var entries []queuedomain.Entry
	if err := cursor.All(ctx, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Service) GetEntryByToken(ctx context.Context, queueID, token string) (*queuedomain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var entry queuedomain.Entry
	if err := s.entryCol.FindOne(ctx, bson.M{"queue_id": queueID, "token": token}).Decode(&entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (s *Service) UpdateEntryStatus(ctx context.Context, queueID, token, status string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.entryCol.UpdateOne(ctx, bson.M{"queue_id": queueID, "token": token}, bson.M{"$set": bson.M{"status": status}})
	return err
}

func (s *Service) CallNextUser(ctx context.Context, queueID string) (*queuedomain.Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tokens, err := s.redisService.GetAllTokensInOrder(ctx, queueID)
	if err != nil || len(tokens) == 0 {
		return nil, fmt.Errorf("no users in queue")
	}
	for _, token := range tokens {
		entry, err := s.GetEntryByToken(ctx, queueID, token)
		if err != nil {
			continue
		}
		if entry.Status == constants.EntryStatusWaiting {
			if err := s.UpdateEntryStatus(ctx, queueID, token, constants.EntryStatusCalled); err != nil {
				return nil, err
			}
			entry.Status = constants.EntryStatusCalled
			return entry, nil
		}
	}
	return nil, fmt.Errorf("no waiting users in queue")
}

func (s *Service) RemoveUser(ctx context.Context, queueID, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.UpdateEntryStatus(ctx, queueID, token, constants.EntryStatusLeft); err != nil {
		return err
	}
	_ = s.redisService.RemoveFromQueue(ctx, queueID, token)
	_ = s.redisService.DeleteUserSession(ctx, queueID, token)
	return nil
}

func (s *Service) SetUserEmail(ctx context.Context, queueID, token, email string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := s.entryCol.UpdateOne(ctx, bson.M{"queue_id": queueID, "token": token}, bson.M{"$set": bson.M{"email": email}})
	return err
}

func (s *Service) SetUserPIN(ctx context.Context, queueID, token, pin string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash PIN: %w", err)
	}
	_, err = s.entryCol.UpdateOne(ctx, bson.M{"queue_id": queueID, "token": token}, bson.M{"$set": bson.M{"pin_hash": string(hash)}})
	return err
}

func (s *Service) RejoinByToken(ctx context.Context, queueID, token string) (*JoinQueueResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exists, err := s.redisService.UserSessionExists(ctx, queueID, token)
	if err != nil || !exists {
		return nil, fmt.Errorf("session expired or invalid")
	}
	entry, err := s.GetEntryByToken(ctx, queueID, token)
	if err != nil {
		return nil, fmt.Errorf("entry not found")
	}
	if entry.Status == constants.EntryStatusServed || entry.Status == constants.EntryStatusLeft || entry.Status == constants.EntryStatusSkipped {
		return nil, fmt.Errorf("your queue session has ended")
	}
	position, _ := s.redisService.GetPosition(ctx, queueID, token)
	queue, _ := s.GetQueue(ctx, queueID)
	avgMins := 5
	if queue != nil {
		avgMins = queue.AvgServiceMins
	}
	return &JoinQueueResult{
		Token:            token,
		TicketNo:         entry.TicketNo,
		Position:         position + 1,
		EstimatedWaitMin: position * int64(avgMins),
	}, nil
}

func (s *Service) RejoinByPIN(ctx context.Context, queueID, ticketNo, pin string) (*JoinQueueResult, error) {
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
	newToken := uuid.New().String()
	_ = s.redisService.SetUserSession(ctx, queueID, newToken, 24*time.Hour)
	if _, err := s.entryCol.UpdateOne(ctx, bson.M{"queue_id": queueID, "ticket_no": ticketNo}, bson.M{"$set": bson.M{"token": newToken}}); err != nil {
		return nil, err
	}
	_ = s.redisService.RemoveFromQueue(ctx, queueID, entry.Token)
	_ = s.redisService.AddToQueue(ctx, queueID, newToken, float64(entry.JoinedAt.Unix()))
	position, _ := s.redisService.GetPosition(ctx, queueID, newToken)
	queue, _ := s.GetQueue(ctx, queueID)
	avgMins := 5
	if queue != nil {
		avgMins = queue.AvgServiceMins
	}
	return &JoinQueueResult{
		Token:            newToken,
		TicketNo:         entry.TicketNo,
		Position:         position + 1,
		EstimatedWaitMin: position * int64(avgMins),
	}, nil
}

func (s *Service) GetActiveQueuesForHost(ctx context.Context, hostPublicID string) ([]queuedomain.Queue, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cursor, err := s.queueCol.Find(ctx, bson.M{"host_public_id": hostPublicID, "status": constants.QueueStatusActive})
	if err != nil {
		return nil, err
	}
	var queues []queuedomain.Queue
	if err := cursor.All(ctx, &queues); err != nil {
		return nil, err
	}
	return queues, nil
}
