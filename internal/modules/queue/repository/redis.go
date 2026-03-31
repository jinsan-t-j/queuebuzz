package repository

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"queuebuzz/internal/constants"
	internalredis "queuebuzz/internal/redis"

	redis "github.com/redis/go-redis/v9"
)

type RedisRepository struct {
	rdb *redis.Client
}

func NewRedisRepository(rdb *redis.Client) *RedisRepository {
	return &RedisRepository{rdb: rdb}
}

// GenerateJoinCode creates a cryptographically random 6-character join code
// from the safe charset, checks Redis for collisions, and stores the mapping.
// Returns the code or an error if all attempts fail.
func (s *RedisRepository) GenerateJoinCode(ctx context.Context, queueID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	charset := constants.JoinCodeCharset
	codeLen := constants.JoinCodeLength
	maxAttempts := constants.MaxJoinCodeAttempts
	ttl := time.Duration(constants.DefaultQueueExpiryH+constants.JoinCodeTTLExtraH) * time.Hour

	for attempt := 0; attempt < maxAttempts; attempt++ {
		code, err := generateRandomCode(charset, codeLen)
		if err != nil {
			return "", fmt.Errorf("failed to generate random code: %w", err)
		}

		key := fmt.Sprintf("joincode:%s", code)

		// Use SET NX to atomically check-and-set (only if key doesn't exist)
		err = s.rdb.SetArgs(ctx, key, queueID, redis.SetArgs{
			Mode: "NX",
			TTL:  ttl,
		}).Err()
		if err == redis.Nil {
			// Collision — try again
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to store join code: %w", err)
		}

		return code, nil
	}

	return "", fmt.Errorf("failed to generate unique join code after %d attempts", maxAttempts)
}

func generateRandomCode(charset string, length int) (string, error) {
	code := make([]byte, length)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := 0; i < length; i++ {
		idx, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", err
		}
		code[i] = charset[idx.Int64()]
	}

	return string(code), nil
}

func (r *RedisRepository) AddToQueue(ctx context.Context, queueID, entryID string, score float64) error {
	key := internalredis.QueuePositionsKey(queueID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.ZAdd(tCtx, key, redis.Z{Score: score, Member: entryID}).Err()
	})
}

func (r *RedisRepository) GetPosition(ctx context.Context, queueID, entryID string) (int64, error) {
	key := internalredis.QueuePositionsKey(queueID)
	rank, err := internalredis.WithRetry(ctx, r.rdb, func(tCtx context.Context) (int64, error) {
		return r.rdb.ZRank(tCtx, key, entryID).Result()
	})
	if err == redis.Nil {
		return -1, nil
	}
	return rank, err
}

func (r *RedisRepository) RemoveFromQueue(ctx context.Context, queueID, entryID string) error {
	key := internalredis.QueuePositionsKey(queueID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.ZRem(tCtx, key, entryID).Err()
	})
}

func (r *RedisRepository) GetSize(ctx context.Context, queueID string) (int64, error) {
	key := internalredis.QueuePositionsKey(queueID)
	return internalredis.WithRetry(ctx, r.rdb, func(tCtx context.Context) (int64, error) {
		return r.rdb.ZCard(tCtx, key).Result()
	})
}

func (r *RedisRepository) GetQueueEntryIDs(ctx context.Context, queueID string) ([]string, error) {
	key := internalredis.QueuePositionsKey(queueID)
	return internalredis.WithRetry(ctx, r.rdb, func(tCtx context.Context) ([]string, error) {
		return r.rdb.ZRange(tCtx, key, 0, -1).Result()
	})
}

func (r *RedisRepository) NextTicket(ctx context.Context, queueID string) (string, error) {
	key := internalredis.TicketCounterKey(queueID)
	num, err := r.rdb.Incr(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("failed to increment ticket counter: %w", err)
	}
	return fmt.Sprintf("Q-%04d", num), nil
}

func (r *RedisRepository) MoveToBack(ctx context.Context, queueID, entryID string) error {
	key := internalredis.QueuePositionsKey(queueID)
	score := float64(time.Now().Unix())
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.ZAdd(tCtx, key, redis.Z{Score: score, Member: entryID}).Err()
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

func (r *RedisRepository) DeleteQueueKeys(ctx context.Context, queueID string) error {
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		keys := []string{
			internalredis.QueuePositionsKey(queueID),
			internalredis.TicketCounterKey(queueID),
		}
		return r.rdb.Del(tCtx, keys...).Err()
	})
}

func (r *RedisRepository) AcquireLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	err := r.rdb.SetArgs(ctx, key, "locked", redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Err()

	if err == redis.Nil {
		// Key already exists (locked)
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

func (r *RedisRepository) ReleaseLock(ctx context.Context, key string) error {
	return r.rdb.Del(ctx, key).Err()
}
