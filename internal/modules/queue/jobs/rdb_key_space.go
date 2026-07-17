package jobs

import (
	"context"
	"time"

	"queuebuzz/internal/log"
	"queuebuzz/internal/modules/queue/service"

	"github.com/redis/go-redis/v9"
)

type KeyspaceJob struct {
	rdb           *redis.Client
	expiryService *service.ExpiryService
}

func NewKeyspaceJob(rdb *redis.Client, s *service.ExpiryService) *KeyspaceJob {
	return &KeyspaceJob{
		rdb:           rdb,
		expiryService: s,
	}
}

// Start runs the Redis keyspace expiry listener inline (blocking).
// Container.runJob wraps this in a goroutine and tracks it via WaitGroup.
func (j *KeyspaceJob) Start(ctx context.Context) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		pubsub := j.rdb.PSubscribe(ctx, "__keyevent@0__:expired")
		ch := pubsub.Channel()
		log.Info().Msg("Redis keyspace expiry listener started")

		for {
			select {
			case <-ctx.Done():
				_ = pubsub.Close()
				return
			case msg, ok := <-ch:
				if !ok {
					// Channel closed — connection lost
					goto reconnect
				}
				backoff = time.Second
				j.expiryService.HandleExpiredKey(ctx, msg.Payload)
			}
		}

	reconnect:
		_ = pubsub.Close()

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		log.Warn().Dur("backoff", backoff).Msg("Keyspace listener connection lost, reconnecting...")

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}
