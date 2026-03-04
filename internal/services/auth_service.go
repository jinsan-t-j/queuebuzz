package services

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/log"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	authInstance *AuthService
	authOnce     sync.Once
)

type AuthService struct {
	privateKey   *rsa.PrivateKey
	publicKey    *rsa.PublicKey
	redisService *RedisService
}

// QueueBuzzClaims represents the JWT claims for the QueueBuzz system.
type QueueBuzzClaims struct {
	Role    string `json:"role"`
	QueueID string `json:"queue_id,omitempty"` // only for anonymous host tokens
	jwt.RegisteredClaims
}

func NewAuthService(privateKeyPEM, publicKeyPEM string, redisService *RedisService) *AuthService {
	authOnce.Do(func() {
		privKey := parseRSAPrivateKey(privateKeyPEM)
		pubKey := parseRSAPublicKey(publicKeyPEM)

		authInstance = &AuthService{
			privateKey:   privKey,
			publicKey:    pubKey,
			redisService: redisService,
		}
	})

	return authInstance
}

// --- Anonymous host token ---

// IssueAnonymousToken creates a JWT for an anonymous host that owns a specific queue.
// The SHA256 hash of the token is stored in Redis for verification.
func (s *AuthService) IssueAnonymousToken(ctx context.Context, queueID string) (string, error) {
	claims := QueueBuzzClaims{
		Role:    constants.RoleAnonymousHost,
		QueueID: queueID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        uuid.New().String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign anonymous token: %w", err)
	}

	// Store SHA256 hash in Redis
	tokenHash := sha256Hash(tokenString)
	if err := s.redisService.SetOwnerToken(ctx, queueID, tokenHash, 24*time.Hour); err != nil {
		return "", fmt.Errorf("failed to store owner token hash: %w", err)
	}

	return tokenString, nil
}

// VerifyAnonymousOwnership verifies that the given JWT's hash matches the stored owner hash.
func (s *AuthService) VerifyAnonymousOwnership(ctx context.Context, tokenString, queueID string) error {
	storedHash, err := s.redisService.GetOwnerToken(ctx, queueID)
	if err != nil {
		return fmt.Errorf("failed to get owner token hash: %w", err)
	}

	if storedHash == "" {
		return fmt.Errorf("no owner token found for queue")
	}

	tokenHash := sha256Hash(tokenString)
	if tokenHash != storedHash {
		return fmt.Errorf("token hash mismatch")
	}

	return nil
}

// --- Registered host tokens ---

// IssueAccessToken creates a short-lived access token for a registered host.
func (s *AuthService) IssueAccessToken(hostID string) (string, time.Time, error) {
	expiresAt := time.Now().Add(15 * time.Minute)

	claims := QueueBuzzClaims{
		Role: constants.RoleRegisteredHost,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   hostID,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        uuid.New().String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to sign access token: %w", err)
	}

	return tokenString, expiresAt, nil
}

// IssueRefreshToken creates a UUID refresh token stored in Redis.
func (s *AuthService) IssueRefreshToken(ctx context.Context, hostID string) (string, time.Time, error) {
	token := uuid.New().String()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	if err := s.redisService.SetRefreshToken(ctx, token, hostID, 7*24*time.Hour); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to store refresh token: %w", err)
	}

	return token, expiresAt, nil
}

// IssueTokenPair creates both an access token and a refresh token.
func (s *AuthService) IssueTokenPair(ctx context.Context, hostID string) (accessToken string, refreshToken string, accessExpiresAt time.Time, refreshExpiresAt time.Time, err error) {
	accessToken, accessExpiresAt, err = s.IssueAccessToken(hostID)
	if err != nil {
		return
	}

	refreshToken, refreshExpiresAt, err = s.IssueRefreshToken(ctx, hostID)
	return
}

// RefreshTokenPair rotates the refresh token — issues new pair, revokes old.
func (s *AuthService) RefreshTokenPair(ctx context.Context, oldRefreshToken string) (accessToken string, newRefreshToken string, accessExpiresAt time.Time, refreshExpiresAt time.Time, err error) {
	hostID, err := s.redisService.GetRefreshToken(ctx, oldRefreshToken)
	if err != nil {
		return "", "", time.Time{}, time.Time{}, fmt.Errorf("failed to get refresh token: %w", err)
	}
	if hostID == "" {
		return "", "", time.Time{}, time.Time{}, fmt.Errorf("invalid or expired refresh token")
	}

	// Revoke old
	_ = s.redisService.DeleteRefreshToken(ctx, oldRefreshToken)

	// Issue new pair
	return s.IssueTokenPair(ctx, hostID)
}

// --- Token verification ---

// VerifyToken parses and validates a JWT token using the RS256 public key.
func (s *AuthService) VerifyToken(tokenString string) (*QueueBuzzClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &QueueBuzzClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.publicKey, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*QueueBuzzClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

// --- Helpers ---

func sha256Hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func parseRSAPrivateKey(pemStr string) *rsa.PrivateKey {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		log.Fatal().Msg("Failed to decode PEM block for private key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS1 format as fallback
		pkcs1Key, err2 := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err2 != nil {
			log.Fatal().Err(err).Msg("Failed to parse private key (tried PKCS8 and PKCS1)")
		}
		return pkcs1Key
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		log.Fatal().Msg("Private key is not RSA")
	}

	return rsaKey
}

func parseRSAPublicKey(pemStr string) *rsa.PublicKey {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		log.Fatal().Msg("Failed to decode PEM block for public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse public key")
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		log.Fatal().Msg("Public key is not RSA")
	}

	return rsaPub
}

// Ensure crypto import is used (for potential future key operations)
var _ = crypto.SHA256
