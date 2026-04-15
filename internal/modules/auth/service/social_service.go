package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"queuebuzz/internal/config"
	authdomain "queuebuzz/internal/modules/auth/domain"
	legacyservices "queuebuzz/internal/services"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
)

const (
	googleCertsURL = "https://www.googleapis.com/oauth2/v3/certs"
	appleKeysURL   = "https://appleid.apple.com/auth/keys"
	appleTokenURL  = "https://appleid.apple.com/auth/token"
	appleAuthURL   = "https://appleid.apple.com/auth/authorize"
	oauthStateTTL  = 10 * time.Minute
	jwksCacheTTL   = time.Hour
)

type socialStatePayload struct {
	Provider string `json:"provider"`
	Nonce    string `json:"nonce"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksResponse struct {
	Keys []jwk `json:"keys"`
}

type jwksCacheEntry struct {
	keys      map[string]*rsa.PublicKey
	expiresAt time.Time
}

type SocialAuthService struct {
	cfg          *config.Config
	redisService *legacyservices.RedisService
	httpClient   *http.Client
	googleConfig *oauth2.Config
	jwksCache    map[string]jwksCacheEntry
	cacheMu      sync.Mutex
}

func NewSocialAuthService(cfg *config.Config, redisService *legacyservices.RedisService) *SocialAuthService {
	return &SocialAuthService{
		cfg:          cfg,
		redisService: redisService,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		googleConfig: &oauth2.Config{
			ClientID:     cfg.GoogleOAuthClientID,
			ClientSecret: cfg.GoogleOAuthClientSecret,
			RedirectURL:  strings.TrimRight(cfg.AppURL, "/") + "/auth/social/google/callback",
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     googleoauth.Endpoint,
		},
		jwksCache: make(map[string]jwksCacheEntry),
	}
}

func (s *SocialAuthService) StartAuth(ctx context.Context, provider string, email string) (string, error) {
	state, err := randomToken(32)
	if err != nil {
		return "", err
	}
	nonce, err := randomToken(32)
	if err != nil {
		return "", err
	}

	payload, err := json.Marshal(socialStatePayload{Provider: provider, Nonce: nonce})
	if err != nil {
		return "", err
	}
	if err := s.redisService.SetOAuthState(ctx, state, string(payload), oauthStateTTL); err != nil {
		return "", err
	}

	switch provider {
	case "google":
		if s.cfg.GoogleOAuthClientID == "" || s.cfg.GoogleOAuthClientSecret == "" {
			return "", fmt.Errorf("google social auth is not configured")
		}
		opts := []oauth2.AuthCodeOption{
			oauth2.AccessTypeOnline,
			oauth2.SetAuthURLParam("nonce", nonce),
		}
		if email != "" {
			opts = append(opts, oauth2.SetAuthURLParam("login_hint", email))
		} else {
			opts = append(opts, oauth2.SetAuthURLParam("prompt", "select_account"))
		}
		return s.googleConfig.AuthCodeURL(state, opts...), nil
	case "apple":
		if s.cfg.AppleOAuthClientID == "" || s.cfg.AppleOAuthTeamID == "" || s.cfg.AppleOAuthKeyID == "" || s.cfg.AppleOAuthPrivateKey == "" {
			return "", fmt.Errorf("apple social auth is not configured")
		}
		params := url.Values{}
		params.Set("client_id", s.cfg.AppleOAuthClientID)
		params.Set("redirect_uri", strings.TrimRight(s.cfg.AppURL, "/")+"/auth/social/apple/callback")
		params.Set("response_type", "code")
		params.Set("response_mode", "form_post")
		params.Set("scope", "name email")
		params.Set("state", state)
		params.Set("nonce", nonce)
		if email != "" {
			params.Set("login_hint", email)
		}
		return appleAuthURL + "?" + params.Encode(), nil
	default:
		return "", fmt.Errorf("unsupported provider")
	}
}

func (s *SocialAuthService) CompleteAuth(ctx context.Context, provider, code, state string) (*authdomain.SocialIdentity, error) {
	stored, err := s.redisService.GetOAuthState(ctx, state)
	if err != nil {
		return nil, err
	}

	if stored == "" {
		return nil, fmt.Errorf("invalid or expired social auth state")
	}
	defer func() { _ = s.redisService.DeleteOAuthState(ctx, state) }()

	var payload socialStatePayload
	if err := json.Unmarshal([]byte(stored), &payload); err != nil {
		return nil, err
	}
	if payload.Provider != provider {
		return nil, fmt.Errorf("social auth provider mismatch")
	}

	switch provider {
	case "google":
		return s.completeGoogleAuth(ctx, code, payload.Nonce)
	case "apple":
		return s.completeAppleAuth(ctx, code, payload.Nonce)
	default:
		return nil, fmt.Errorf("unsupported provider")
	}
}

func (s *SocialAuthService) completeGoogleAuth(ctx context.Context, code, nonce string) (*authdomain.SocialIdentity, error) {
	token, err := s.googleConfig.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("google code exchange failed: %w", err)
	}
	idToken, _ := token.Extra("id_token").(string)
	if idToken == "" {
		return nil, errors.New("google did not return an id_token")
	}

	claims, err := s.verifyIDToken(idToken, googleCertsURL, []string{s.cfg.GoogleOAuthClientID}, []string{"https://accounts.google.com", "accounts.google.com"}, nonce)
	if err != nil {
		return nil, err
	}

	identity := &authdomain.SocialIdentity{
		Provider:       "google",
		ProviderUserID: stringClaim(claims, "sub"),
		Email:          stringClaim(claims, "email"),
		EmailVerified:  boolClaim(claims, "email_verified"),
		Name:           stringClaim(claims, "name"),
	}
	if identity.ProviderUserID == "" || identity.Email == "" || !identity.EmailVerified {
		return nil, errors.New("google account did not return a verified email")
	}

	return identity, nil
}

func (s *SocialAuthService) completeAppleAuth(ctx context.Context, code, nonce string) (*authdomain.SocialIdentity, error) {
	clientSecret, err := s.buildAppleClientSecret()
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("client_id", s.cfg.AppleOAuthClientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", strings.TrimRight(s.cfg.AppURL, "/")+"/auth/social/apple/callback")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, appleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apple token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("apple token exchange failed with status %d", resp.StatusCode)
	}

	var tokenResp struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	if tokenResp.IDToken == "" {
		return nil, errors.New("apple did not return an id_token")
	}

	claims, err := s.verifyIDToken(tokenResp.IDToken, appleKeysURL, []string{s.cfg.AppleOAuthClientID}, []string{"https://appleid.apple.com"}, nonce)
	if err != nil {
		return nil, err
	}

	email := stringClaim(claims, "email")
	emailVerified := boolClaim(claims, "email_verified")
	return &authdomain.SocialIdentity{
		Provider:       "apple",
		ProviderUserID: stringClaim(claims, "sub"),
		Email:          email,
		EmailVerified:  emailVerified,
		Name:           email,
	}, nil
}

func (s *SocialAuthService) buildAppleClientSecret() (string, error) {
	key, err := parseApplePrivateKey(s.cfg.AppleOAuthPrivateKey)
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    s.cfg.AppleOAuthTeamID,
		Subject:   s.cfg.AppleOAuthClientID,
		Audience:  jwt.ClaimStrings{"https://appleid.apple.com"},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
	}
	jwtToken := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	jwtToken.Header["kid"] = s.cfg.AppleOAuthKeyID
	return jwtToken.SignedString(key)
}

func parseApplePrivateKey(raw string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return nil, errors.New("failed to decode apple oauth private key")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ecdsaKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("apple oauth private key is not ECDSA")
		}
		return ecdsaKey, nil
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func (s *SocialAuthService) verifyIDToken(idToken, jwksURL string, audience []string, issuers []string, nonce string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(idToken, claims, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("token header missing kid")
		}
		keys, err := s.fetchJWKS(jwksURL)
		if err != nil {
			return nil, err
		}
		key, ok := keys[kid]
		if !ok {
			return nil, errors.New("signing key not found")
		}
		return key, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid id_token")
	}
	if !containsString(issuers, stringClaim(claims, "iss")) {
		return nil, errors.New("unexpected token issuer")
	}
	if !containsAudience(claims["aud"], audience) {
		return nil, errors.New("unexpected token audience")
	}
	if nonce != "" && stringClaim(claims, "nonce") != nonce {
		return nil, errors.New("social auth nonce mismatch")
	}
	if err := validateExp(claims); err != nil {
		return nil, err
	}
	return claims, nil
}

func (s *SocialAuthService) fetchJWKS(jwksURL string) (map[string]*rsa.PublicKey, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	if entry, ok := s.jwksCache[jwksURL]; ok && time.Now().Before(entry.expiresAt) {
		return entry.keys, nil
	}

	resp, err := s.httpClient.Get(jwksURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	keys := make(map[string]*rsa.PublicKey, len(payload.Keys))
	for _, key := range payload.Keys {
		pubKey, err := jwkToPublicKey(key)
		if err != nil {
			return nil, err
		}
		keys[key.Kid] = pubKey
	}

	s.jwksCache[jwksURL] = jwksCacheEntry{
		keys:      keys,
		expiresAt: time.Now().Add(jwksCacheTTL),
	}
	return keys, nil
}

func jwkToPublicKey(key jwk) (*rsa.PublicKey, error) {
	if key.Kty != "RSA" {
		return nil, errors.New("unsupported jwk kty")
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(key.N)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(key.E)
	if err != nil {
		return nil, err
	}

	var exponent int
	for _, b := range eBytes {
		exponent = exponent<<8 + int(b)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: exponent,
	}, nil
}

func stringClaim(claims jwt.MapClaims, key string) string {
	value, _ := claims[key].(string)
	return value
}

func boolClaim(claims jwt.MapClaims, key string) bool {
	switch value := claims[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(value, "true")
	default:
		return false
	}
}

func randomToken(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(buf)), nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsAudience(raw any, allowed []string) bool {
	switch value := raw.(type) {
	case string:
		return containsString(allowed, value)
	case []any:
		for _, item := range value {
			if str, ok := item.(string); ok && containsString(allowed, str) {
				return true
			}
		}
	}
	return false
}

func validateExp(claims jwt.MapClaims) error {
	rawExp, ok := claims["exp"]
	if !ok {
		return errors.New("token missing exp")
	}
	var exp int64
	switch value := rawExp.(type) {
	case float64:
		exp = int64(value)
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return err
		}
		exp = parsed
	default:
		return errors.New("invalid exp claim")
	}
	if time.Now().After(time.Unix(exp, 0)) {
		return errors.New("token expired")
	}
	return nil
}
