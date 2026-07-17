package repository

import (
	"context"
	"time"

	internalredis "queuebuzz/internal/redis"

	redis "github.com/redis/go-redis/v9"
)

type RedisRepository struct {
	rdb *redis.Client
}

func NewRedisRepository(rdb *redis.Client) *RedisRepository {
	return &RedisRepository{rdb: rdb}
}

func (r *RedisRepository) Client() *redis.Client {
	return r.rdb
}

func (r *RedisRepository) SetUserSession(ctx context.Context, queueID, entryID string, ttl time.Duration) error {
	key := internalredis.UserSessionKey(queueID, entryID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.Set(tCtx, key, "1", ttl).Err()
	})
}

func (r *RedisRepository) UserSessionExists(ctx context.Context, queueID, entryID string) (bool, error) {
	key := internalredis.UserSessionKey(queueID, entryID)
	n, err := internalredis.WithRetry(ctx, r.rdb, func(tCtx context.Context) (int64, error) {
		return r.rdb.Exists(tCtx, key).Result()
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *RedisRepository) DeleteUserSession(ctx context.Context, queueID, entryID string) error {
	key := internalredis.UserSessionKey(queueID, entryID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.Del(tCtx, key).Err()
	})
}

func (r *RedisRepository) SetIdleTimer(ctx context.Context, queueID, entryID string, ttl time.Duration) error {
	key := internalredis.IdleTimerKey(queueID, entryID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.Set(tCtx, key, "1", ttl).Err()
	})
}

func (r *RedisRepository) SetGraceTimer(ctx context.Context, queueID, entryID string, ttl time.Duration) error {
	key := internalredis.GraceTimerKey(queueID, entryID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.Set(tCtx, key, "1", ttl).Err()
	})
}

func (r *RedisRepository) ClearIdleTimer(ctx context.Context, queueID, entryID string) error {
	key := internalredis.IdleTimerKey(queueID, entryID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.Del(tCtx, key).Err()
	})
}

func (r *RedisRepository) ClearGraceTimer(ctx context.Context, queueID, entryID string) error {
	key := internalredis.GraceTimerKey(queueID, entryID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.Del(tCtx, key).Err()
	})
}
