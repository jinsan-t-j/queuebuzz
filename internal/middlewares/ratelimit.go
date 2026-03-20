package middlewares

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
)

// RateLimiter creates a configurable rate limiting middleware.
func RateLimiter(max int, expiration time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: expiration,
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

// Predefined rate limiters per the security checklist
var (
	// JoinRateLimiter — 5 req / IP / min for join endpoints
	JoinRateLimiter = RateLimiter(5, 1*time.Minute)

	// RegisterRateLimiter — 5 req / IP / 5 min for registration
	RegisterRateLimiter = RateLimiter(5, 5*time.Minute)

	// VerifyRateLimiter — 5 req / IP / 15 min for auth verification
	VerifyRateLimiter = RateLimiter(5, 15*time.Minute)

	// SSERateLimiter — 10 connections / IP / min for SSE streams
	SSERateLimiter = RateLimiter(10, 1*time.Minute)

	// GlobalRateLimiter — 100 req / IP / min for all endpoints
	GlobalRateLimiter = RateLimiter(100, 1*time.Minute)

	// LenientRateLimiter — 30 req / IP / min for general endpoints
	LenientRateLimiter = RateLimiter(30, 1*time.Minute)
)
