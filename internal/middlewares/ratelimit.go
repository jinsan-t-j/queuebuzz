package middlewares

import (
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
func NewRateLimiters(cfg *config.Config) *RateLimiters {
	disabled := cfg.DisableRateLimit

	factory := func(limit int, expiration time.Duration) fiber.Handler {
		return limiter.New(limiter.Config{
			Max:        limit,
			Expiration: expiration,
			Next: func(_ fiber.Ctx) bool {
				return disabled
			},
			KeyGenerator: func(c fiber.Ctx) string {
				return c.IP()
			},
			LimitReached: func(c fiber.Ctx) error {
				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"message": "Too many requests. Please try again later.",
				})
			},
		})
	}

	return &RateLimiters{
		Join:     factory(5, 1*time.Minute),
		Register: factory(5, 5*time.Minute),
		Verify:   factory(5, 15*time.Minute),
		SSE:      factory(10, 1*time.Minute),
		Global:   factory(100, 1*time.Minute),
		Lenient:  factory(30, 1*time.Minute),
		Host:     factory(2, 1*time.Second),
	}
}
