package service

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type QueueRedisService struct {
	rdb *redis.Client
}

func NewQueueRedisService(rdb *redis.Client) *QueueRedisService {
	return &QueueRedisService{rdb: rdb}
}

func (s *QueueRedisService) NextTicket(ctx context.Context, queueID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("ticket_counter:%s", queueID)
	num, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("failed to increment ticket counter: %w", err)
	}

	return fmt.Sprintf("Q-%04d", num), nil
}
