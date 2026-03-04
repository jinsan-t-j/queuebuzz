package services

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"

	"queuebuzz/internal/constants"

	"github.com/redis/go-redis/v9"
)

var (
	joinCodeInstance *JoinCodeService
	joinCodeOnce     sync.Once
)

type JoinCodeService struct {
	rdb *redis.Client
}

func NewJoinCodeService(rdb *redis.Client) *JoinCodeService {
	joinCodeOnce.Do(func() {
		joinCodeInstance = &JoinCodeService{rdb: rdb}
	})

	return joinCodeInstance
}

// GenerateJoinCode creates a cryptographically random 6-character join code
// from the safe charset, checks Redis for collisions, and stores the mapping.
// Returns the code or an error if all attempts fail.
func (s *JoinCodeService) GenerateJoinCode(ctx context.Context, queueID string) (string, error) {
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
		ok, err := s.rdb.SetNX(ctx, key, queueID, ttl).Result()
		if err != nil {
			return "", fmt.Errorf("failed to store join code: %w", err)
		}

		if ok {
			return code, nil
		}
		// Collision — try again
	}

	return "", fmt.Errorf("failed to generate unique join code after %d attempts", maxAttempts)
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
