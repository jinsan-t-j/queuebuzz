package config

import (
	"os"
	"sync"

	"queuebuzz/internal/log"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	AppURL  string `env:"APP_URL" env-default:"http://localhost:8080"`
	AppPort string `env:"APP_PORT" env-default:"8080"`
	AppEnv  string `env:"APP_ENV" env-default:"development"`

	// MongoDB
	DBUri  string `env:"DB_URI" env-required:"true"`
	DBName string `env:"DB_NAME" env-required:"true"`

	// Redis
	RedisURL      string `env:"REDIS_URL" env-required:"true"`
	RedisPassword string `env:"REDIS_PASSWORD" env-default:""`

	// JWT RS256
	JWTPrivateKey string `env:"JWT_PRIVATE_KEY" env-required:"true"`
	JWTPublicKey  string `env:"JWT_PUBLIC_KEY" env-required:"true"`

	// Firebase
	FirebaseCredentials string `env:"FIREBASE_CREDENTIALS" env-required:"true"`

	// Email
	ResendAPIKey    string `env:"RESEND_API_KEY" env-default:""`
	EmailFrom       string `env:"EMAIL_FROM" env-default:"noreply@queuebuzz.com"`
	EmailReplyTo    string `env:"EMAIL_REPLY_TO" env-default:""`
	MailpitSMTPHost string `env:"MAILPIT_SMTP_HOST" env-default:"localhost"`
	MailpitSMTPPort string `env:"MAILPIT_SMTP_PORT" env-default:"1025"`

	// CORS
	AllowedOrigin string `env:"ALLOWED_ORIGIN" env-required:"true"`

	// FRONTEND
	AuthCallbackURL string `env:"AUTH_CALLBACK_URL" env-required:"true"`

	// Social Auth
	GoogleOAuthClientID     string `env:"GOOGLE_OAUTH_CLIENT_ID"`
	GoogleOAuthClientSecret string `env:"GOOGLE_OAUTH_CLIENT_SECRET"`
	AppleOAuthClientID      string `env:"APPLE_OAUTH_CLIENT_ID" env-default:""`
	AppleOAuthTeamID        string `env:"APPLE_OAUTH_TEAM_ID" env-default:""`
	AppleOAuthKeyID         string `env:"APPLE_OAUTH_KEY_ID" env-default:""`
	AppleOAuthPrivateKey    string `env:"APPLE_OAUTH_PRIVATE_KEY" env-default:""`
}

var (
	cfg  *Config
	once sync.Once
)

func Get() *Config {
	once.Do(func() {
		cfg = &Config{}
		_ = cleanenv.ReadConfig(".env", cfg)

		if err := cleanenv.ReadEnv(cfg); err != nil {
			log.Fatal().Err(err).Msg("Config error loading environment variables")
		}

		if port := os.Getenv("PORT"); port != "" {
			cfg.AppPort = port
		}
	})

	return cfg
}

func (c *Config) IsProduction() bool {
	return c.AppEnv == "production"
}
