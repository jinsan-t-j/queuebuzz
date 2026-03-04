package services

import (
	"context"
	"strings"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/log"
	"queuebuzz/internal/ws"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// ExpiryListener manages Redis keyspace notifications and queue expiry cleanup.
type ExpiryListener struct {
	rdb          *redis.Client
	queueCol     *mongo.Collection
	entryCol     *mongo.Collection
	redisService *RedisService
	hub          *ws.Hub
	notifSender  NotificationSender
}

// NewExpiryListener creates a new expiry listener.
func NewExpiryListener(
	rdb *redis.Client,
	queueCol *mongo.Collection,
	entryCol *mongo.Collection,
	redisSvc *RedisService,
	hub *ws.Hub,
	notifSender NotificationSender,
) *ExpiryListener {
	return &ExpiryListener{
		rdb:          rdb,
		queueCol:     queueCol,
		entryCol:     entryCol,
		redisService: redisSvc,
		hub:          hub,
		notifSender:  notifSender,
	}
}

// StartKeyspaceListener subscribes to Redis keyspace notifications for expired keys.
// This runs in a goroutine and handles idle_timer, grace_timer, and queue expiry events.
func (l *ExpiryListener) StartKeyspaceListener(ctx context.Context) {
	// Subscribe to expired key events on db 0
	pubsub := l.rdb.PSubscribe(ctx, "__keyevent@0__:expired")

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
				l.handleExpiredKey(ctx, msg.Payload)
			}
		}
	}()

	log.Info().Msg("Redis keyspace expiry listener started")
}

func (l *ExpiryListener) handleExpiredKey(ctx context.Context, key string) {
	switch {
	case strings.HasPrefix(key, "idle_timer:"):
		l.handleIdleTimerExpiry(ctx, key)
	case strings.HasPrefix(key, "grace_timer:"):
		l.handleGraceTimerExpiry(ctx, key)
	}
}

func (l *ExpiryListener) handleIdleTimerExpiry(ctx context.Context, key string) {
	// key format: idle_timer:{queue_id}:{token}
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return
	}
	queueID, token := parts[1], parts[2]

	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Update status to IDLE
	_, err := l.entryCol.UpdateOne(opCtx,
		bson.M{"queue_id": queueID, "token": token},
		bson.M{"$set": bson.M{"status": constants.EntryStatusIdle}},
	)
	if err != nil {
		log.Error().Err(err).Str("queue_id", queueID).Msg("Failed to set entry to IDLE")
		return
	}

	// Move user to back of sorted set
	_ = l.redisService.MoveToBack(opCtx, queueID, token)

	// Set grace timer (5 min)
	_ = l.redisService.SetGraceTimer(opCtx, queueID, token,
		time.Duration(constants.DefaultGraceTimerSec)*time.Second)

	// Broadcast status change via WebSocket
	l.hub.Broadcast(queueID, ws.Message{
		Event: ws.EventUserStatusChanged,
		Data:  ws.UserStatusData{Token: token, Status: constants.EntryStatusIdle},
	})

	log.Info().Str("queue_id", queueID).Str("token_prefix", token[:8]).Msg("User moved to IDLE")
}

func (l *ExpiryListener) handleGraceTimerExpiry(ctx context.Context, key string) {
	// key format: grace_timer:{queue_id}:{token}
	parts := strings.SplitN(key, ":", 3)
	if len(parts) != 3 {
		return
	}
	queueID, token := parts[1], parts[2]

	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Update status to SKIPPED
	_, err := l.entryCol.UpdateOne(opCtx,
		bson.M{"queue_id": queueID, "token": token},
		bson.M{"$set": bson.M{"status": constants.EntryStatusSkipped}},
	)
	if err != nil {
		log.Error().Err(err).Str("queue_id", queueID).Msg("Failed to set entry to SKIPPED")
		return
	}

	// Remove from sorted set
	_ = l.redisService.RemoveFromQueue(opCtx, queueID, token)

	// Broadcast via WebSocket
	l.hub.Broadcast(queueID, ws.Message{
		Event: ws.EventUserStatusChanged,
		Data:  ws.UserStatusData{Token: token, Status: constants.EntryStatusSkipped},
	})

	log.Info().Str("queue_id", queueID).Str("token_prefix", token[:8]).Msg("User SKIPPED after grace period")
}

// RunCronSweep checks for expired queues and cleans them up.
// Acts as a fallback for missed Redis keyspace events.
func (l *ExpiryListener) RunCronSweep(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.sweepExpiredQueues(ctx)
		}
	}
}

func (l *ExpiryListener) sweepExpiredQueues(ctx context.Context) {
	opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Find active queues that have passed their expiry time
	cursor, err := l.queueCol.Find(opCtx, bson.M{
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
		l.expireQueue(ctx, q.ID, q.JoinCode)
	}

	if len(expired) > 0 {
		log.Info().Int("count", len(expired)).Msg("Cron sweep: expired queues cleaned up")
	}
}

func (l *ExpiryListener) expireQueue(ctx context.Context, queueID, joinCode string) {
	opCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// 1. Update queue status to EXPIRED
	_, _ = l.queueCol.UpdateOne(opCtx,
		bson.M{"_id": queueID},
		bson.M{"$set": bson.M{"status": constants.QueueStatusExpired}},
	)

	// 2. Delete join code from Redis
	delCtx, delCancel := context.WithTimeout(ctx, 5*time.Second)
	defer delCancel()
	_ = l.rdb.Del(delCtx, "joincode:"+joinCode).Err()

	// 3. Disconnect WebSocket clients
	l.hub.DisconnectQueue(queueID)
	l.hub.Broadcast(queueID, ws.Message{
		Event: ws.EventQueueExpired,
		Data:  ws.QueueExpiredData{QueueID: queueID},
	})

	// 4. Clean up all Redis keys for this queue
	_ = l.redisService.DeleteQueueKeys(opCtx, queueID)

	// 5. Clear email fields from entries (privacy cleanup)
	_, _ = l.entryCol.UpdateMany(opCtx,
		bson.M{"queue_id": queueID},
		bson.M{"$unset": bson.M{"email": ""}},
	)

	log.Info().Str("queue_id", queueID).Msg("Queue expired and cleaned up")
}
