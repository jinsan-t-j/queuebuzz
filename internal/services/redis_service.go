package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	redisServiceInstance *RedisService
	redisServiceOnce     sync.Once
)

type RedisService struct {
	rdb *redis.Client
}

func NewRedisService(rdb *redis.Client) *RedisService {
	redisServiceOnce.Do(func() {
		redisServiceInstance = &RedisService{rdb: rdb}
	})

	return redisServiceInstance
}

// --- Sorted set position management ---

// AddToQueue adds a user token to the queue's sorted set with the given score (unix timestamp).
func (s *RedisService) AddToQueue(ctx context.Context, queueID, token string, score float64) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("queue_positions:%s", queueID)
	return s.rdb.ZAdd(ctx, key, redis.Z{Score: score, Member: token}).Err()
}

// RemoveFromQueue removes a user token from the queue's sorted set.
func (s *RedisService) RemoveFromQueue(ctx context.Context, queueID, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("queue_positions:%s", queueID)
	return s.rdb.ZRem(ctx, key, token).Err()
}

// GetPosition returns the 0-based rank of a token in the queue's sorted set.
// Returns -1 if the token is not found.
func (s *RedisService) GetPosition(ctx context.Context, queueID, token string) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("queue_positions:%s", queueID)
	rank, err := s.rdb.ZRank(ctx, key, token).Result()
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
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("queue_positions:%s", queueID)
	return s.rdb.ZCard(ctx, key).Result()
}

// GetAllTokensInOrder returns all tokens in the queue sorted by score (join time).
func (s *RedisService) GetAllTokensInOrder(ctx context.Context, queueID string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("queue_positions:%s", queueID)
	return s.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:   key,
		Start: 0,
		Stop:  -1,
	}).Result()
}

// MoveToBack moves a token to the end of the sorted set by re-adding with current timestamp.
func (s *RedisService) MoveToBack(ctx context.Context, queueID, token string) error {
	return s.AddToQueue(ctx, queueID, token, float64(time.Now().Unix()))
}

// --- User session management ---

// SetUserSession stores a user session token in Redis with TTL.
func (s *RedisService) SetUserSession(ctx context.Context, queueID, token string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("user_session:%s:%s", queueID, token)
	return s.rdb.Set(ctx, key, "1", ttl).Err()
}

// UserSessionExists checks if a user session token exists.
func (s *RedisService) UserSessionExists(ctx context.Context, queueID, token string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("user_session:%s:%s", queueID, token)
	n, err := s.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteUserSession removes a user session.
func (s *RedisService) DeleteUserSession(ctx context.Context, queueID, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("user_session:%s:%s", queueID, token)
	return s.rdb.Del(ctx, key).Err()
}

// --- Heartbeat management ---

// SetHeartbeat sets the user's active status with TTL.
func (s *RedisService) SetHeartbeat(ctx context.Context, queueID, token string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("user_active:%s:%s", queueID, token)
	return s.rdb.Set(ctx, key, "1", ttl).Err()
}

// IsUserActive checks if the user has a recent heartbeat.
func (s *RedisService) IsUserActive(ctx context.Context, queueID, token string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("user_active:%s:%s", queueID, token)
	n, err := s.rdb.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// --- Owner token management ---

// SetOwnerToken stores the SHA256 hash of the anonymous host's JWT.
func (s *RedisService) SetOwnerToken(ctx context.Context, queueID, tokenHash string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("owner:%s", queueID)
	return s.rdb.Set(ctx, key, tokenHash, ttl).Err()
}

// GetOwnerToken retrieves the stored owner token hash.
func (s *RedisService) GetOwnerToken(ctx context.Context, queueID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("owner:%s", queueID)
	val, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

// DeleteOwnerToken removes the owner token hash.
func (s *RedisService) DeleteOwnerToken(ctx context.Context, queueID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("owner:%s", queueID)
	return s.rdb.Del(ctx, key).Err()
}

// --- Idle / Grace timer management ---

// SetIdleTimer sets an idle timer for a called user.
func (s *RedisService) SetIdleTimer(ctx context.Context, queueID, token string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("idle_timer:%s:%s", queueID, token)
	return s.rdb.Set(ctx, key, "1", ttl).Err()
}

// SetGraceTimer sets a grace timer for an idle user.
func (s *RedisService) SetGraceTimer(ctx context.Context, queueID, token string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("grace_timer:%s:%s", queueID, token)
	return s.rdb.Set(ctx, key, "1", ttl).Err()
}

// --- Refresh token management ---

// SetRefreshToken stores a refresh token mapping in Redis.
func (s *RedisService) SetRefreshToken(ctx context.Context, token, hostID string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("refresh:%s", token)
	return s.rdb.Set(ctx, key, hostID, ttl).Err()
}

// GetRefreshToken retrieves the host ID for a refresh token.
func (s *RedisService) GetRefreshToken(ctx context.Context, token string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("refresh:%s", token)
	val, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

// DeleteRefreshToken removes a refresh token from Redis.
func (s *RedisService) DeleteRefreshToken(ctx context.Context, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("refresh:%s", token)
	return s.rdb.Del(ctx, key).Err()
}

// --- OAuth state management ---

func (s *RedisService) SetOAuthState(ctx context.Context, state, payload string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("oauth_state:%s", state)
	return s.rdb.Set(ctx, key, payload, ttl).Err()
}

func (s *RedisService) GetOAuthState(ctx context.Context, state string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("oauth_state:%s", state)
	val, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return val, err
}

func (s *RedisService) DeleteOAuthState(ctx context.Context, state string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("oauth_state:%s", state)
	return s.rdb.Del(ctx, key).Err()
}

// --- Cleanup helpers ---

// DeleteQueueKeys cleans up all Redis keys associated with a queue.
func (s *RedisService) DeleteQueueKeys(ctx context.Context, queueID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	patterns := []string{
		fmt.Sprintf("queue_positions:%s", queueID),
		fmt.Sprintf("ticket_counter:%s", queueID),
		fmt.Sprintf("owner:%s", queueID),
	}

	pipe := s.rdb.Pipeline()
	for _, key := range patterns {
		pipe.Del(ctx, key)
	}

	// Delete all user session and heartbeat keys for this queue
	for _, prefix := range []string{"user_session:", "user_active:", "idle_timer:", "grace_timer:"} {
		pattern := fmt.Sprintf("%s%s:*", prefix, queueID)
		iter := s.rdb.Scan(ctx, 0, pattern, 100).Iterator()
		for iter.Next(ctx) {
			pipe.Del(ctx, iter.Val())
		}
	}

	_, err := pipe.Exec(ctx)
	return err
}
