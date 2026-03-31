package services

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type JoinCodeService struct {
	rdb *redis.Client
}

func NewJoinCodeService(rdb *redis.Client) *JoinCodeService {
	return &JoinCodeService{rdb: rdb}
}

// ResolveJoinCode looks up a join code in Redis and returns the associated queue ID.
func (s *JoinCodeService) ResolveJoinCode(ctx context.Context, code string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("joincode:%s", code)
	queueID, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to resolve join code: %w", err)
	}

	return queueID, nil
}

// DeleteJoinCode removes a join code from Redis.
func (s *JoinCodeService) DeleteJoinCode(ctx context.Context, code string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("joincode:%s", code)
	return s.rdb.Del(ctx, key).Err()
}
