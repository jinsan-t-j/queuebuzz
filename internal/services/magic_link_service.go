package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type MagicLinkService struct {
	rdb *redis.Client
}

func NewMagicLinkService(rdb *redis.Client) *MagicLinkService {
	return &MagicLinkService{rdb: rdb}
}

// GenerateAndStoreMagicLink creates a UUID magic link token, stores the
// associated email in Redis with a 15-minute TTL.
func (s *MagicLinkService) GenerateAndStoreMagicLink(ctx context.Context, email string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	token := uuid.New().String()
	key := fmt.Sprintf("magic:%s", token)

	if err := s.rdb.Set(ctx, key, email, 15*time.Minute).Err(); err != nil {
		return "", fmt.Errorf("failed to store magic link: %w", err)
	}

	return token, nil
}

// VerifyMagicLink verifies the magic link token and returns the associated email.
// Single-use — the key is deleted after successful verification.
func (s *MagicLinkService) VerifyMagicLink(ctx context.Context, token string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("magic:%s", token)
	email, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", fmt.Errorf("magic link expired or invalid")
	}
	if err != nil {
		return "", fmt.Errorf("failed to verify magic link: %w", err)
	}

	// Single use — delete after verification
	s.rdb.Del(ctx, key)

	return email, nil
}
