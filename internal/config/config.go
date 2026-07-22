package config

import (
	"fmt"
	"os"
	"sync"

	"queuebuzz/internal/log"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	AppURL  string `env:"APP_URL" env-default:"http://localhost:8080"`
	AppPort string `env:"APP_PORT" env-default:"8080"`
	AppEnv  string `env:"APP_ENV" env-default:"development"`

	// Database (Oracle NoSQL / Mongo-compatible interface)
	DBUri              string `env:"DB_URI" env-required:"true"`
	DBName             string `env:"DB_NAME" env-required:"true"`
	MongoDBMaxPoolSize uint64 `env:"MONGO_MAX_POOL_SIZE" env-default:"500"`

	// Redis
	RedisURL           string `env:"REDIS_URL" env-required:"true"`
	RedisPassword      string `env:"REDIS_PASSWORD" env-default:""`
	RedisMaxPoolSize   int    `env:"REDIS_MAX_POOL_SIZE" env-default:"500"`
	RedisMinIdleConns  int    `env:"REDIS_MIN_IDLE_CONNS" env-default:"50"`
	RedisStorePoolSize int    `env:"REDIS_STORE_POOL_SIZE" env-default:"500"`

	// JWT RS256
	JWTPrivateKey string `env:"JWT_PRIVATE_KEY" env-required:"true"`
	JWTPublicKey  string `env:"JWT_PUBLIC_KEY" env-required:"true"`

	// Firebase
	FirebaseCredentials string `env:"FIREBASE_CREDENTIALS" env-required:"true"`

	// Email
	BrevoAPIKey     string `env:"BREVO_API_KEY" env-default:""`
	DodoAPIKey      string `env:"DODO_API_KEY"`
	DodoWebhookKey  string `env:"DODO_WEBHOOK_KEY"`
	EmailFrom       string `env:"EMAIL_FROM" env-default:"noreply@queuebuzz.com"`
	EmailReplyTo    string `env:"EMAIL_REPLY_TO" env-default:""`
	MailpitSMTPHost string `env:"MAILPIT_SMTP_HOST" env-default:"localhost"`
	MailpitSMTPPort string `env:"MAILPIT_SMTP_PORT" env-default:"1025"`

	// CORS
	AllowedOrigin string `env:"ALLOWED_ORIGIN" env-required:"true"`

	// FRONTEND
	AuthCallbackURL string `env:"AUTH_CALLBACK_URL" env-required:"true"`
	FrontendURL     string `env:"FRONTEND_URL" env-default:"https://queuebuzz.com"`

	// Social Auth
	GoogleOAuthClientID     string `env:"GOOGLE_OAUTH_CLIENT_ID"`
	GoogleOAuthClientSecret string `env:"GOOGLE_OAUTH_CLIENT_SECRET"`
	AppleOAuthClientID      string `env:"APPLE_OAUTH_CLIENT_ID" env-default:""`
	AppleOAuthTeamID        string `env:"APPLE_OAUTH_TEAM_ID" env-default:""`
	AppleOAuthKeyID         string `env:"APPLE_OAUTH_KEY_ID" env-default:""`
	AppleOAuthPrivateKey    string `env:"APPLE_OAUTH_PRIVATE_KEY" env-default:""`

	// R2 Storage
	R2AccessKeyID     string `env:"R2_ACCESS_KEY_ID" env-required:"true"`
	R2SecretAccessKey string `env:"R2_SECRET_ACCESS_KEY" env-required:"true"`
	R2BucketName      string `env:"R2_BUCKET_NAME" env-required:"true"`
	R2Endpoint        string `env:"R2_ENDPOINT" env-required:"true"`
	R2PublicURL       string `env:"R2_PUBLIC_URL" env-required:"true"`

	// Security
	SystemAPISecret    string `env:"SYSTEM_API_SECRET" env-required:"true"`
	SupportEmail       string `env:"SUPPORT_EMAIL" env-default:"support@queuebuzz.com"`
	RecoveryHMACSecret string `env:"RECOVERY_HMAC_SECRET" env-required:"true"`

	// Sentry
	SentryDSN              string  `env:"SENTRY_DSN" env-default:""`
	SentryTracesSampleRate float64 `env:"SENTRY_TRACES_SAMPLE_RATE" env-default:"0.1"`

	// Testing
	DisableRateLimit        bool   `env:"DISABLE_RATE_LIMIT" env-default:"false"`
	OverrideQueueGuardToken string `env:"OVERIDE_QUEUE_GUARD_TOKEN" env-default:""`

	// Rate limits. Defaults intentionally preserve the current production posture.
	RateLimitJoinPerMinute     int `env:"RATE_LIMIT_JOIN_PER_MINUTE" env-default:"20"`
	RateLimitRegisterPerMinute int `env:"RATE_LIMIT_REGISTER_PER_MINUTE" env-default:"20"`
	RateLimitVerifyPerMinute   int `env:"RATE_LIMIT_VERIFY_PER_MINUTE" env-default:"30"`
	RateLimitSSEPerMinute      int `env:"RATE_LIMIT_SSE_PER_MINUTE" env-default:"10"`
	RateLimitGlobalPerMinute   int `env:"RATE_LIMIT_GLOBAL_PER_MINUTE" env-default:"100"`
	RateLimitLenientPerMinute  int `env:"RATE_LIMIT_LENIENT_PER_MINUTE" env-default:"30"`
	RateLimitHostPerSecond     int `env:"RATE_LIMIT_HOST_PER_SECOND" env-default:"2"`
}

var (
	cfg  *Config
	once sync.Once
)

func Get() *Config {
	once.Do(func() {
		cfg = Load()
	})

	return cfg
}

func Load() *Config {
	c := &Config{}
	_ = cleanenv.ReadConfig(".env", c)

	if err := cleanenv.ReadEnv(c); err != nil {
		fmt.Fprintf(os.Stderr, "CONFIG ERROR (env): %v\n", err)
		log.Fatal().Err(err).Msg("Config error loading environment variables")
	}

	if port := os.Getenv("PORT"); port != "" {
		c.AppPort = port
	}

	return c
}

func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}

func (c *Config) IsTesting() bool {
	return c.AppEnv == "test"
}
