package repository

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/log"
	"queuebuzz/internal/modules/queue/domain"
	internalredis "queuebuzz/internal/redis"

	redis "github.com/redis/go-redis/v9"
)

func (r *RedisRepository) SetHistorySummary(ctx context.Context, hostPublicID string, summary *domain.HostHistorySummary) error {
	key := fmt.Sprintf("history_summary:%s", hostPublicID)
	data, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	return r.rdb.Set(ctx, key, data, 24*time.Hour).Err()
}

func (r *RedisRepository) GetHistorySummary(ctx context.Context, hostPublicID string) (*domain.HostHistorySummary, error) {
	key := fmt.Sprintf("history_summary:%s", hostPublicID)
	data, err := r.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	var summary domain.HostHistorySummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func (r *RedisRepository) InvalidateHistorySummary(ctx context.Context, hostPublicID string) error {
	key := fmt.Sprintf("history_summary:%s", hostPublicID)
	return r.rdb.Del(ctx, key).Err()
}

func (r *RedisRepository) SetHistoryDetail(ctx context.Context, queueID string, detail interface{}) error {
	key := fmt.Sprintf("history_detail:%s", queueID)
	data, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	return r.rdb.Set(ctx, key, data, 7*24*time.Hour).Err()
}

func (r *RedisRepository) GetHistoryDetail(ctx context.Context, queueID string) ([]byte, error) {
	key := fmt.Sprintf("history_detail:%s", queueID)
	return r.rdb.Get(ctx, key).Bytes()
}

type RedisRepository struct {
	rdb *redis.Client
}

func NewRedisRepository(rdb *redis.Client) *RedisRepository {
	return &RedisRepository{rdb: rdb}
}

// GenerateJoinCode creates a cryptographically random 6-character join code
// from the safe charset, checks Redis for collisions, and stores the mapping.
// Returns the code or an error if all attempts fail.
func (r *RedisRepository) ReleaseJoinCode(ctx context.Context, code string) error {
	key := fmt.Sprintf("joincode:%s", code)
	return r.rdb.Del(ctx, key).Err()
}

func (r *RedisRepository) GenerateJoinCode(ctx context.Context, queueID string, length int, maxAttempts int, ttl time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	charset := constants.JoinCodeCharset
	if length <= 0 {
		length = constants.JoinCodeLength
	}
	if maxAttempts <= 0 {
		maxAttempts = constants.MaxJoinCodeAttempts
	}

	if ttl <= 0 {
		ttl = time.Duration(constants.DefaultQueueExpiryH+constants.JoinCodeTTLExtraH) * time.Hour
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		code, err := generateRandomCode(charset, length)
		if err != nil {
			return "", fmt.Errorf("failed to generate random code: %w", err)
		}

		key := fmt.Sprintf("joincode:%s", code)

		// Use SET NX to atomically check-and-set (only if key doesn't exist)
		err = r.rdb.SetArgs(ctx, key, queueID, redis.SetArgs{
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

func (r *RedisRepository) AddToQueue(ctx context.Context, queueID, entryID string, score float64, ttl time.Duration) error {
	key := internalredis.QueuePositionsKey(queueID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		err := r.rdb.ZAdd(tCtx, key, redis.Z{Score: score, Member: entryID}).Err()
		if err == nil && ttl > 0 {
			r.rdb.Expire(tCtx, key, ttl)
		}
		return err
	})
}

func (r *RedisRepository) AddToQueueBulk(ctx context.Context, queueID string, members []redis.Z, ttl time.Duration) error {
	if len(members) == 0 {
		return nil
	}
	key := internalredis.QueuePositionsKey(queueID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		err := r.rdb.ZAdd(tCtx, key, members...).Err()
		if err == nil && ttl > 0 {
			r.rdb.Expire(tCtx, key, ttl)
		}
		return err
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
	return r.GetQueueEntryIDsRange(ctx, queueID, 0, -1)
}

func (r *RedisRepository) GetQueueEntryIDsRange(ctx context.Context, queueID string, start, stop int64) ([]string, error) {
	key := internalredis.QueuePositionsKey(queueID)
	return internalredis.WithRetry(ctx, r.rdb, func(tCtx context.Context) ([]string, error) {
		return r.rdb.ZRange(tCtx, key, start, stop).Result()
	})
}

func (r *RedisRepository) NextTicket(ctx context.Context, queueID string, ttl time.Duration) (string, error) {
	key := internalredis.TicketCounterKey(queueID)
	num, err := r.rdb.Incr(ctx, key).Result()
	if err != nil {
		return "", fmt.Errorf("failed to increment ticket counter: %w", err)
	}
	if ttl > 0 {
		r.rdb.Expire(ctx, key, ttl)
	}
	return fmt.Sprintf("Q-%04d", num), nil
}

func (r *RedisRepository) HasTicketCounter(ctx context.Context, queueID string) (bool, error) {
	key := internalredis.TicketCounterKey(queueID)
	val, err := r.rdb.Exists(ctx, key).Result()
	return val > 0, err
}

func (r *RedisRepository) SetTicketCounter(ctx context.Context, queueID string, value int64, ttl time.Duration) error {
	key := internalredis.TicketCounterKey(queueID)
	return r.rdb.Set(ctx, key, value, ttl).Err()
}

func (r *RedisRepository) MoveToBack(ctx context.Context, queueID, entryID string) error {
	key := internalredis.QueuePositionsKey(queueID)
	score := float64(time.Now().Unix())
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		return r.rdb.ZAdd(tCtx, key, redis.Z{Score: score, Member: entryID}).Err()
	})
}

func (r *RedisRepository) RepositionEntry(ctx context.Context, queueID, entryID string, offset int64) error {
	key := internalredis.QueuePositionsKey(queueID)
	return internalredis.ExecRetry(ctx, r.rdb, func(tCtx context.Context) error {
		// Find the score of the member at the target offset position
		members, err := r.rdb.ZRangeWithScores(tCtx, key, offset, offset).Result()
		if err != nil && err != redis.Nil {
			return err
		}

		var newScore float64
		if len(members) == 0 {
			// If queue is shorter than offset, just put at end
			maxScore, _ := r.rdb.ZRangeWithScores(tCtx, key, -1, -1).Result()
			if len(maxScore) > 0 {
				newScore = maxScore[0].Score + 1
			} else {
				newScore = float64(time.Now().Unix())
			}
		} else {
			// Move to just after the target offset member
			newScore = members[0].Score + 1
		}

		return r.rdb.ZAdd(tCtx, key, redis.Z{Score: newScore, Member: entryID}).Err()
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

// SetRehydratedSentinel marks a queue as recently synced with MongoDB.
// This acts as a "Negative Cache" or throttle to prevent multiple concurrent requests
// from triggering redundant re-hydration queries against the database (Thundering Herd).
func (r *RedisRepository) SetRehydratedSentinel(ctx context.Context, queueID string, ttl time.Duration) error {
	key := fmt.Sprintf("rehydrated:%s", queueID)
	log.Debug().Str("queue_id", queueID).Dur("ttl", ttl).Msg("Redis: setting re-hydration sentinel")
	return r.rdb.Set(ctx, key, "1", ttl).Err()
}

// HasRehydratedSentinel checks if the queue was recently re-hydrated from MongoDB.
// If returns true, the caller should assume the database was already checked
// and found to be empty, avoiding unnecessary database load.
func (r *RedisRepository) HasRehydratedSentinel(ctx context.Context, queueID string) (bool, error) {
	key := fmt.Sprintf("rehydrated:%s", queueID)
	val, err := r.rdb.Exists(ctx, key).Result()
	if err == nil && val > 0 {
		log.Debug().Str("queue_id", queueID).Msg("Redis: re-hydration sentinel hit (throttled)")
	}
	return val > 0, err
}
