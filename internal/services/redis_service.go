package services

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisService struct {
	rdb *redis.Client
}

func NewRedisService(rdb *redis.Client) *RedisService {
	return &RedisService{rdb: rdb}
}

// --- Common Retry Helpers ---

func withRetry[T any](ctx context.Context, op func(context.Context) (T, error)) (T, error) {
	var result T
	var err error

	for i := 0; i < 3; i++ {
		tCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		result, err = op(tCtx)
		cancel()

		if err == nil || err == redis.Nil {
			return result, err
		}

		// Don't retry if the parent request context is already cancelled/expired
		if ctx.Err() != nil {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	return result, err
}

func execRetry(ctx context.Context, op func(context.Context) error) error {
	var err error

	for i := 0; i < 3; i++ {
		tCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = op(tCtx)
		cancel()

		if err == nil || err == redis.Nil {
			return err
		}

		// Don't retry if the parent request context is already cancelled/expired
		if ctx.Err() != nil {
			break
		}

		time.Sleep(50 * time.Millisecond)
	}

	return err
}

// --- Sorted set position management ---

// AddToQueue adds a user token to the queue's sorted set with the given score (unix timestamp).
func (s *RedisService) AddToQueue(ctx context.Context, queueID, token string, score float64) error {
	key := fmt.Sprintf("queue_positions:%s", queueID)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.ZAdd(tCtx, key, redis.Z{Score: score, Member: token}).Err()
	})
}

// RemoveFromQueue removes a user token from the queue's sorted set.
func (s *RedisService) RemoveFromQueue(ctx context.Context, queueID, token string) error {
	key := fmt.Sprintf("queue_positions:%s", queueID)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.ZRem(tCtx, key, token).Err()
	})
}

// GetPosition returns the 0-based rank of a token in the queue's sorted set.
// Returns -1 if the token is not found.
func (s *RedisService) GetPosition(ctx context.Context, queueID, token string) (int64, error) {
	key := fmt.Sprintf("queue_positions:%s", queueID)
	rank, err := withRetry(ctx, func(tCtx context.Context) (int64, error) {
		return s.rdb.ZRank(tCtx, key, token).Result()
	})

	if err == redis.Nil {
		return -1, nil
	}
	if err != nil {
		return -1, err
	}
	return rank, nil
}

// GetQueueSize returns the number of members in the queue's sorted set.
func (s *RedisService) GetQueueSize(ctx context.Context, queueID string) (int64, error) {
	key := fmt.Sprintf("queue_positions:%s", queueID)
	return withRetry(ctx, func(tCtx context.Context) (int64, error) {
		return s.rdb.ZCard(tCtx, key).Result()
	})
}

// GetAllQueuePositionsInOrder returns all tokens in the queue sorted by score (join time).
func (s *RedisService) GetAllQueuePositionsInOrder(ctx context.Context, queueID string) ([]string, error) {
	key := fmt.Sprintf("queue_positions:%s", queueID)
	return withRetry(ctx, func(tCtx context.Context) ([]string, error) {
		return s.rdb.ZRangeArgs(tCtx, redis.ZRangeArgs{
			Key:   key,
			Start: 0,
			Stop:  -1,
		}).Result()
	})
}

// MoveToBack moves a token to the end of the sorted set by re-adding with current timestamp.
func (s *RedisService) MoveToBack(ctx context.Context, queueID, token string) error {
	return s.AddToQueue(ctx, queueID, token, float64(time.Now().Unix()))
}

// --- User session management ---

// SetUserSession stores a user session token in Redis with TTL.
func (s *RedisService) SetUserSession(ctx context.Context, queueID, token string, ttl time.Duration) error {
	key := fmt.Sprintf("user_session:%s:%s", queueID, token)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Set(tCtx, key, "1", ttl).Err()
	})
}

// UserSessionExists checks if a user session token exists.
func (s *RedisService) UserSessionExists(ctx context.Context, queueID, token string) (bool, error) {
	key := fmt.Sprintf("user_session:%s:%s", queueID, token)
	n, err := withRetry(ctx, func(tCtx context.Context) (int64, error) {
		return s.rdb.Exists(tCtx, key).Result()
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteUserSession removes a user session.
func (s *RedisService) DeleteUserSession(ctx context.Context, queueID, token string) error {
	key := fmt.Sprintf("user_session:%s:%s", queueID, token)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Del(tCtx, key).Err()
	})
}

// --- Heartbeat management ---

// SetHeartbeat sets the user's active status with TTL.
func (s *RedisService) SetHeartbeat(ctx context.Context, queueID, token string, ttl time.Duration) error {
	key := fmt.Sprintf("user_active:%s:%s", queueID, token)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Set(tCtx, key, "1", ttl).Err()
	})
}

// IsUserActive checks if the user has a recent heartbeat.
func (s *RedisService) IsUserActive(ctx context.Context, queueID, token string) (bool, error) {
	key := fmt.Sprintf("user_active:%s:%s", queueID, token)
	n, err := withRetry(ctx, func(tCtx context.Context) (int64, error) {
		return s.rdb.Exists(tCtx, key).Result()
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// --- Anon host token management ---

// SetAnonHostToken stores the SHA256 hash of the anonymous host's JWT.
func (s *RedisService) SetAnonHostToken(ctx context.Context, queueID, tokenHash string, ttl time.Duration) error {
	key := fmt.Sprintf("anon_host:%s", queueID)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Set(tCtx, key, tokenHash, ttl).Err()
	})
}

// GetAnonHostToken retrieves the stored anon host token hash.
func (s *RedisService) GetAnonHostToken(ctx context.Context, queueID string) (string, error) {
	key := fmt.Sprintf("anon_host:%s", queueID)
	val, err := withRetry(ctx, func(tCtx context.Context) (string, error) {
		return s.rdb.Get(tCtx, key).Result()
	})

	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

// DeleteAnonHostToken removes the anon host token hash.
func (s *RedisService) DeleteAnonHostToken(ctx context.Context, queueID string) error {
	key := fmt.Sprintf("anon_host:%s", queueID)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Del(tCtx, key).Err()
	})
}

// --- Idle / Grace timer management ---

// SetIdleTimer sets an idle timer for a called user.
func (s *RedisService) SetIdleTimer(ctx context.Context, queueID, token string, ttl time.Duration) error {
	key := fmt.Sprintf("idle_timer:%s:%s", queueID, token)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Set(tCtx, key, "1", ttl).Err()
	})
}

// SetGraceTimer sets a grace timer for an idle user.
func (s *RedisService) SetGraceTimer(ctx context.Context, queueID, token string, ttl time.Duration) error {
	key := fmt.Sprintf("grace_timer:%s:%s", queueID, token)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Set(tCtx, key, "1", ttl).Err()
	})
}

// --- Refresh token management ---

// SetRefreshToken stores a refresh token mapping in Redis.
func (s *RedisService) SetRefreshToken(ctx context.Context, token, hostID string, ttl time.Duration) error {
	key := fmt.Sprintf("refresh:%s", token)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Set(tCtx, key, hostID, ttl).Err()
	})
}

// GetRefreshToken retrieves the host ID for a refresh token.
func (s *RedisService) GetRefreshToken(ctx context.Context, token string) (string, error) {
	key := fmt.Sprintf("refresh:%s", token)
	val, err := withRetry(ctx, func(tCtx context.Context) (string, error) {
		return s.rdb.Get(tCtx, key).Result()
	})

	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

// DeleteRefreshToken removes a refresh token from Redis.
func (s *RedisService) DeleteRefreshToken(ctx context.Context, token string) error {
	key := fmt.Sprintf("refresh:%s", token)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Del(tCtx, key).Err()
	})
}

// --- OAuth state management ---

func (s *RedisService) SetOAuthState(ctx context.Context, state, payload string, ttl time.Duration) error {
	key := fmt.Sprintf("oauth_state:%s", state)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Set(tCtx, key, payload, ttl).Err()
	})
}

func (s *RedisService) GetOAuthState(ctx context.Context, state string) (string, error) {
	key := fmt.Sprintf("oauth_state:%s", state)
	val, err := withRetry(ctx, func(tCtx context.Context) (string, error) {
		return s.rdb.Get(tCtx, key).Result()
	})

	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (s *RedisService) DeleteOAuthState(ctx context.Context, state string) error {
	key := fmt.Sprintf("oauth_state:%s", state)
	return execRetry(ctx, func(tCtx context.Context) error {
		return s.rdb.Del(tCtx, key).Err()
	})
}

// --- Cleanup helpers ---

// DeleteQueueKeys cleans up all Redis keys associated with a queue.
func (s *RedisService) DeleteQueueKeys(ctx context.Context, queueID string) error {
	return execRetry(ctx, func(tCtx context.Context) error {
		patterns := []string{
			fmt.Sprintf("queue_positions:%s", queueID),
			fmt.Sprintf("ticket_counter:%s", queueID),
			fmt.Sprintf("anon_host:%s", queueID),
		}

		pipe := s.rdb.Pipeline()
		for _, key := range patterns {
			pipe.Del(tCtx, key)
		}

		// Delete all user session and heartbeat keys for this queue
		for _, prefix := range []string{"user_session:", "user_active:", "idle_timer:", "grace_timer:"} {
			pattern := fmt.Sprintf("%s%s:*", prefix, queueID)
			iter := s.rdb.Scan(tCtx, 0, pattern, 100).Iterator()
			for iter.Next(tCtx) {
				pipe.Del(tCtx, iter.Val())
			}
		}

		_, err := pipe.Exec(tCtx)
		return err
	})
}
