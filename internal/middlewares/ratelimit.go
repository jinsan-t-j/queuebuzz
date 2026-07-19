package middlewares

import (
	"crypto/subtle"
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
				if c.Path() == "/healthz" {
					return true
				}
				bypassToken := cfg.OverrideQueueGuardToken
				if bypassToken == "" {
					bypassToken = os.Getenv("OVERIDE_QUEUE_GUARD_TOKEN")
				}
				clientToken := c.Get("X-Bypass-Active-Queue-Guard")
				if bypassToken != "" && clientToken != "" &&
					subtle.ConstantTimeCompare([]byte(bypassToken), []byte(clientToken)) == 1 {
					return true
				}
				return false
			},
			KeyGenerator: func(c fiber.Ctx) string {
				scope := c.Params("id")
				if scope == "" {
					scope = c.Query("id")
				}
				if hostID, ok := c.Locals("host_id").(string); ok && hostID != "" {
					scope = hostID + ":" + scope
				}
				if scope == "" {
					scope = "global"
				}
				return prefix + ":" + c.IP() + ":" + scope
			},
			LimitReached: func(c fiber.Ctx) error {
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"message": "Too many requests. Please try again later.",
				})
			},
		})
	}

	return &RateLimiters{
		Join:     factory("join", cfg.RateLimitJoinPerMinute, 1*time.Minute),
		Register: factory("register", cfg.RateLimitRegisterPerMinute, 1*time.Minute),
		Verify:   factory("verify", cfg.RateLimitVerifyPerMinute, 1*time.Minute),
		SSE:      factory("sse", cfg.RateLimitSSEPerMinute, 1*time.Minute),
		Global:   factory("global", cfg.RateLimitGlobalPerMinute, 1*time.Minute),
		Lenient:  factory("lenient", cfg.RateLimitLenientPerMinute, 1*time.Minute),
		Host:     factory("host", cfg.RateLimitHostPerSecond, 1*time.Second),
	}
}
