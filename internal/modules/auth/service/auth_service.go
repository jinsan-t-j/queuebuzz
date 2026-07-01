package service

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/log"
	legacyservices "queuebuzz/internal/services"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type AuthService struct {
	privateKey   *rsa.PrivateKey
	publicKey    *rsa.PublicKey
	redisService *legacyservices.RedisService
}

type QueueBuzzClaims struct {
	Role     string `json:"role"`
	QueueID  string `json:"queue_id,omitempty"`
	PublicID string `json:"public_id,omitempty"`
	EntryID  string `json:"entry_id,omitempty"`
	jwt.RegisteredClaims
}

func NewAuthService(privateKeyPEM, publicKeyPEM string, redisService *legacyservices.RedisService) *AuthService {
	return &AuthService{
		privateKey:   parseRSAPrivateKey(privateKeyPEM),
		publicKey:    parseRSAPublicKey(publicKeyPEM),
		redisService: redisService,
	}
}

func (s *AuthService) IssueGuestEntryToken(queueID, entryID string) (string, error) {
	claims := QueueBuzzClaims{
		Role:    "guest",
		QueueID: queueID,
		EntryID: entryID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "queuebuzz",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        uuid.New().String(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign guest token: %w", err)
	}

	return tokenString, nil
}

func (s *AuthService) IssueAnonymousToken(ctx context.Context, queueID string) (string, error) {
	claims := QueueBuzzClaims{
		Role:    constants.RoleAnonymousHost,
		QueueID: queueID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "queuebuzz",
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

	tokenHash := sha256Hash(tokenString)
	if err := s.redisService.SetAnonHostToken(ctx, queueID, tokenHash, 24*time.Hour); err != nil {
		return "", fmt.Errorf("failed to store anon host token hash: %w", err)
	}

	return tokenString, nil
}

func (s *AuthService) VerifyAnonymousOwnership(ctx context.Context, tokenString, queueID string) error {
	storedHash, err := s.redisService.GetAnonHostToken(ctx, queueID)
	if err != nil {
		return fmt.Errorf("failed to get anon host token hash: %w", err)
	}

	if storedHash == "" {
		return fmt.Errorf("no active session found for queue")
	}

	if sha256Hash(tokenString) != storedHash {
		return fmt.Errorf("token hash mismatch: session replaced or invalid")
	}
	return nil
}

func (s *AuthService) ReSyncAnonymousSession(ctx context.Context, tokenString, queueID string) error {
	tokenHash := sha256Hash(tokenString)
	return s.redisService.SetAnonHostToken(ctx, queueID, tokenHash, 24*time.Hour)
}

func (s *AuthService) IssueAccessToken(hostID, publicID string) (string, time.Time, error) {
	expiresAt := time.Now().Add(15 * time.Minute)

	claims := QueueBuzzClaims{
		Role: constants.RoleRegisteredHost,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "queuebuzz",
			Subject:   hostID,
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			ID:        uuid.New().String(),
		},
		PublicID: publicID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tokenString, err := token.SignedString(s.privateKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to sign access token: %w", err)
	}

	return tokenString, expiresAt, nil
}

func (s *AuthService) IssueRefreshToken(ctx context.Context, hostID, publicID string) (string, time.Time, error) {
	token := uuid.New().String()
	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	payload := hostID
	if publicID != "" {
		payload = fmt.Sprintf("%s:%s", hostID, publicID)
	}

	if err := s.redisService.SetRefreshToken(ctx, token, payload, 7*24*time.Hour); err != nil {
		return "", time.Time{}, fmt.Errorf("failed to store refresh token: %w", err)
	}

	return token, expiresAt, nil
}

func (s *AuthService) IssueTokenPair(ctx context.Context, hostID, publicID string) (string, string, time.Time, time.Time, error) {
	accessToken, accessExpiresAt, err := s.IssueAccessToken(hostID, publicID)
	if err != nil {
		return "", "", time.Time{}, time.Time{}, err
	}

	refreshToken, refreshExpiresAt, err := s.IssueRefreshToken(ctx, hostID, publicID)
	if err != nil {
		return "", "", time.Time{}, time.Time{}, err
	}

	return accessToken, refreshToken, accessExpiresAt, refreshExpiresAt, nil
}

func (s *AuthService) RefreshTokenPair(ctx context.Context, oldRefreshToken string) (string, string, time.Time, time.Time, error) {
	payload, err := s.redisService.GetRefreshToken(ctx, oldRefreshToken)
	if err != nil {
		return "", "", time.Time{}, time.Time{}, fmt.Errorf("failed to get refresh token: %w", err)
	}
	if payload == "" {
		return "", "", time.Time{}, time.Time{}, fmt.Errorf("invalid or expired refresh token")
	}

	_ = s.redisService.DeleteRefreshToken(ctx, oldRefreshToken)

	hostID := payload
	publicID := ""

	if strings.Contains(payload, ":") {
		parts := strings.SplitN(payload, ":", 2)
		hostID = parts[0]
		publicID = parts[1]
	}

	return s.IssueTokenPair(ctx, hostID, publicID)
}

func (s *AuthService) VerifyToken(tokenString string) (*QueueBuzzClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &QueueBuzzClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method != jwt.SigningMethodRS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.publicKey, nil
	}, jwt.WithIssuer("queuebuzz"))
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*QueueBuzzClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

func sha256Hash(data string) string {
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func parseRSAPrivateKey(pemStr string) *rsa.PrivateKey {
	normalized := normalizePEM(pemStr, "-----BEGIN PRIVATE KEY-----", "-----END PRIVATE KEY-----")
	block, _ := pem.Decode([]byte(normalized))
	if block == nil {
		first25 := ""
		if len(pemStr) > 25 {
			first25 = pemStr[:25]
		} else {
			first25 = pemStr
		}
		last25 := ""
		if len(pemStr) > 25 {
			last25 = pemStr[len(pemStr)-25:]
		} else {
			last25 = pemStr
		}
		log.Fatal().
			Int("length", len(pemStr)).
			Str("first25", first25).
			Str("last25", last25).
			Bool("contains_literal_slash_n", strings.Contains(pemStr, `\n`)).
			Bool("contains_real_newline", strings.Contains(pemStr, "\n")).
			Msg("Failed to decode PEM block for private key")
		return nil
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
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
	normalized := normalizePEM(pemStr, "-----BEGIN PUBLIC KEY-----", "-----END PUBLIC KEY-----")
	block, _ := pem.Decode([]byte(normalized))
	if block == nil {
		log.Fatal().Msg("Failed to decode PEM block for public key")
		return nil
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

func normalizePEM(pemStr string, header, footer string) string {
	pemStr = strings.ReplaceAll(pemStr, `\n`, "\n")
	pemStr = strings.Trim(pemStr, "\"`' \n\r\t")

	if block, _ := pem.Decode([]byte(pemStr)); block != nil {
		return pemStr
	}

	cleaned := strings.ReplaceAll(pemStr, "\n", "")
	cleaned = strings.ReplaceAll(cleaned, "\r", "")

	if !strings.HasPrefix(cleaned, header) || !strings.HasSuffix(cleaned, footer) {
		return pemStr
	}

	body := cleaned
	body = strings.TrimPrefix(body, header)
	body = strings.TrimSuffix(body, footer)
	body = strings.TrimSpace(body)
	body = strings.ReplaceAll(body, " ", "")

	var formattedBody strings.Builder
	for i := 0; i < len(body); i += 64 {
		end := i + 64
		if end > len(body) {
			end = len(body)
		}
		formattedBody.WriteString(body[i:end])
		formattedBody.WriteString("\n")
	}

	return fmt.Sprintf("%s\n%s%s\n", header, formattedBody.String(), footer)
}
