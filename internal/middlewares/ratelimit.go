package middlewares

import (
	"os"
	"time"

	"queuebuzz/internal/config"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
)

// RateLimiters holds all pre-configured rate limiting middlewares.
type RateLimiters struct {
	Join     fiber.Handler
	Register fiber.Handler
	Verify   fiber.Handler
	SSE      fiber.Handler
	Global   fiber.Handler
	Lenient  fiber.Handler
	Host     fiber.Handler
}

// NewRateLimiters initializes rate limiters based on the provided configuration.
func NewRateLimiters(cfg *config.Config, store fiber.Storage) *RateLimiters {
	disabled := cfg.DisableRateLimit

	factory := func(prefix string, limit int, expiration time.Duration) fiber.Handler {
		return limiter.New(limiter.Config{
			Max:        limit,
			Expiration: expiration,
			Storage:    store,
			Next: func(c fiber.Ctx) bool {
				if disabled {
					return true
				}
				bypassToken := cfg.OverrideQueueGuardToken
				if bypassToken == "" {
					bypassToken = os.Getenv("OVERIDE_QUEUE_GUARD_TOKEN")
				}
				clientToken := c.Get("X-Bypass-Active-Queue-Guard")
				if bypassToken != "" && clientToken != "" && bypassToken == clientToken {
					return true
				}
				return false
			},
			KeyGenerator: func(c fiber.Ctx) string {
				return prefix + ":" + c.IP()
			},
			LimitReached: func(c fiber.Ctx) error {
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"message": "Too many requests. Please try again later.",
				})
			},
		})
	}

	return &RateLimiters{
		Join:     factory("join", 20, 1*time.Minute),
		Register: factory("register", 20, 1*time.Minute),
		Verify:   factory("verify", 30, 1*time.Minute),
		SSE:      factory("sse", 10, 1*time.Minute),
		Global:   factory("global", 100, 1*time.Minute),
		Lenient:  factory("lenient", 30, 1*time.Minute),
		Host:     factory("host", 2, 1*time.Second),
	}
}
