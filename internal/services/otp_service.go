package services

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type OTPService struct {
	rdb *redis.Client
}

func NewOTPService(rdb *redis.Client) *OTPService {
	return &OTPService{rdb: rdb}
}

// GenerateAndStoreOTP creates a 6-digit OTP using crypto/rand, bcrypt-hashes it,
// and stores the hash in Redis with a 10-minute TTL.
func (s *OTPService) GenerateAndStoreOTP(ctx context.Context, phone string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	otp, err := generateOTP(6)
	if err != nil {
		return "", fmt.Errorf("failed to generate OTP: %w", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(otp), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("failed to hash OTP: %w", err)
	}

	key := fmt.Sprintf("otp:%s", phone)
	attemptsKey := fmt.Sprintf("otp_attempts:%s", phone)

	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, key, string(hash), 10*time.Minute)
	pipe.Set(ctx, attemptsKey, "0", 10*time.Minute)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to store OTP: %w", err)
	}

	return otp, nil
}

// VerifyOTP checks the provided OTP against the stored bcrypt hash.
// Single-use — the key is deleted after successful verification.
// Returns an error if the OTP is wrong, expired, or max attempts exceeded.
func (s *OTPService) VerifyOTP(ctx context.Context, phone, otp string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	attemptsKey := fmt.Sprintf("otp_attempts:%s", phone)

	// Check brute force protection
	attempts, err := s.rdb.Incr(ctx, attemptsKey).Result()
	if err != nil {
		return fmt.Errorf("failed to check attempts: %w", err)
	}
	if attempts > 5 {
		// Delete both keys to force re-request
		s.rdb.Del(ctx, fmt.Sprintf("otp:%s", phone), attemptsKey)
		return fmt.Errorf("max verification attempts exceeded")
	}

	key := fmt.Sprintf("otp:%s", phone)
	storedHash, err := s.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return fmt.Errorf("OTP expired or not found")
	}
	if err != nil {
		return fmt.Errorf("failed to get OTP: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(otp)); err != nil {
		return fmt.Errorf("invalid OTP")
	}

	// Single use — delete after success
	s.rdb.Del(ctx, key, attemptsKey)

	return nil
}

func generateOTP(length int) (string, error) {
	otp := make([]byte, length)
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		otp[i] = '0' + byte(n.Int64())
	}
	return string(otp), nil
}
