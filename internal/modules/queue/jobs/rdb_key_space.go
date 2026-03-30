package jobs

import (
	"context"

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

func (j *KeyspaceJob) Start(ctx context.Context) {
	pubsub := j.rdb.PSubscribe(ctx, "__keyevent@0__:expired")

	go func() {
		defer pubsub.Close()

		ch := pubsub.Channel()
		log.Info().Msg("Redis keyspace expiry listener started")

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				j.expiryService.HandleExpiredKey(ctx, msg.Payload)
			}
		}
	}()
}
