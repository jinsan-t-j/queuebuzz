package service

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	legacyservices "queuebuzz/internal/services"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

const (
	recoveryTokenTTL = 2 * time.Hour
	recoveryPrefix   = "recovery:"
)

type RecoveryService struct {
	rdb      *redis.Client
	entryCol *mongodriver.Collection
	emailSvc *legacyservices.EmailService
	hmacKey  []byte
	frontURL string
}

func NewRecoveryService(
	rdb *redis.Client,
	entryCol *mongodriver.Collection,
	emailSvc *legacyservices.EmailService,
	hmacSecret string,
	frontendURL string,
) *RecoveryService {
	return &RecoveryService{
		rdb:      rdb,
		entryCol: entryCol,
		emailSvc: emailSvc,
		hmacKey:  []byte(hmacSecret),
		frontURL: strings.TrimRight(frontendURL, "/"),
	}
}

// DispatchRecoveryEmail generates a one-time recovery token and sends the email.
func (s *RecoveryService) DispatchRecoveryEmail(
	ctx context.Context, entryID, queueID, email, queueName string,
) error {
	res, err := s.entryCol.UpdateOne(ctx,
		bson.M{
			"_id":                    entryID,
			"recovery_email_sent_at": bson.M{"$exists": false},
		},
		bson.M{
			"$set": bson.M{
				"recovery_email_sent_at": time.Now(),
			},
		},
	)
	if err != nil {
		return fmt.Errorf("failed to mark recovery email as sent: %w", err)
	}
	if res.ModifiedCount == 0 {
		return nil
	}

	token, err := s.GenerateToken(ctx, entryID, queueID)
	if err != nil {
		_, _ = s.entryCol.UpdateOne(ctx,
			bson.M{"_id": entryID},
			bson.M{"$unset": bson.M{"recovery_email_sent_at": ""}},
		)
		return err
	}

	link := fmt.Sprintf("%s/q/%s/recover?token=%s", s.frontURL, queueID, token)
	return s.emailSvc.SendSessionRecovery(email, queueName, link)
}

// GenerateToken creates a signed token and stores its hash for single-use claim.
func (s *RecoveryService) GenerateToken(ctx context.Context, entryID, queueID string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("crypto/rand failed: %w", err)
	}

	payload := hex.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.hmacKey)
	_, _ = mac.Write(raw)
	sig := hex.EncodeToString(mac.Sum(nil))
	token := payload + "." + sig

	tokenHash := sha256Hex(token)
	val := fmt.Sprintf("%s:%s", entryID, queueID)
	if err := s.rdb.Set(ctx, recoveryPrefix+tokenHash, val, recoveryTokenTTL).Err(); err != nil {
		return "", fmt.Errorf("failed to store recovery token: %w", err)
	}

	return token, nil
}

// ClaimToken validates and atomically consumes a recovery token.
func (s *RecoveryService) ClaimToken(ctx context.Context, token string) (string, string, error) {
	parts := splitToken(token)
	if parts == nil {
		return "", "", fmt.Errorf("malformed recovery token")
	}

	rawBytes, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", "", fmt.Errorf("malformed recovery token")
	}

	mac := hmac.New(sha256.New, s.hmacKey)
	_, _ = mac.Write(rawBytes)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return "", "", fmt.Errorf("invalid recovery token signature")
	}

	tokenHash := sha256Hex(token)
	val, err := s.rdb.GetDel(ctx, recoveryPrefix+tokenHash).Result()
	if err == redis.Nil {
		return "", "", fmt.Errorf("recovery token expired or already used")
	}
	if err != nil {
		return "", "", fmt.Errorf("failed to claim token: %w", err)
	}

	entryID, queueID := parseVal(val)
	if entryID == "" || queueID == "" {
		return "", "", fmt.Errorf("corrupted recovery token data")
	}

	return entryID, queueID, nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func splitToken(t string) []string {
	for i := len(t) - 1; i >= 0; i-- {
		if t[i] == '.' {
			return []string{t[:i], t[i+1:]}
		}
	}
	return nil
}

func parseVal(v string) (string, string) {
	for i := 0; i < len(v); i++ {
		if v[i] == ':' {
			return v[:i], v[i+1:]
		}
	}
	return "", ""
}
