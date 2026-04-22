package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type MagicLinkService struct {
	rdb *redis.Client
}

type MagicLinkPayload struct {
	Email        string `json:"email"`
	ClaimQueueID string `json:"claim_queue_id,omitempty"`
}

func NewMagicLinkService(rdb *redis.Client) *MagicLinkService {
	return &MagicLinkService{rdb: rdb}
}

// GenerateAndStoreMagicLink creates a UUID magic link token, stores the
// associated email and metadata in Redis with a 15-minute TTL.
func (s *MagicLinkService) GenerateAndStoreMagicLink(ctx context.Context, email string, claimQueueID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	token := uuid.New().String()
	key := fmt.Sprintf("magic:%s", token)

	payload, err := json.Marshal(MagicLinkPayload{Email: email, ClaimQueueID: claimQueueID})
	if err != nil {
		return "", fmt.Errorf("failed to marshal magic link payload: %w", err)
	}

	if err := s.rdb.Set(ctx, key, string(payload), 15*time.Minute).Err(); err != nil {
		return "", fmt.Errorf("failed to store magic link: %w", err)
	}

	return token, nil
}

// VerifyMagicLink verifies the magic link token and returns the associated payload.
// Single-use — the key is deleted after successful verification.
func (s *MagicLinkService) VerifyMagicLink(ctx context.Context, token string) (*MagicLinkPayload, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("magic:%s", token)
	val, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, fmt.Errorf("magic link expired or invalid")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to verify magic link: %w", err)
	}

	var payload MagicLinkPayload
	if err := json.Unmarshal([]byte(val), &payload); err != nil {
		// Fallback for legacy plain-email tokens if any exist (optional, but safer)
		return &MagicLinkPayload{Email: val}, nil
	}

	// Single use — delete after verification
	s.rdb.Del(ctx, key)

	return &payload, nil
}
