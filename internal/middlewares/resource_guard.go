package middlewares

import (
	"strings"

	"queuebuzz/internal/limit"

	"github.com/gofiber/fiber/v3"
)

var mutatingSemaphore = make(chan struct{}, 1000)

// bypassPaths are critical external-provider paths that must never be shed.
var bypassPaths = []string{
	"/api/v1/billing/webhook",
}

// ResourceGuardMiddleware sheds mutating write load under system resource exhaustion.
func ResourceGuardMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		// 1. Bypass GET and OPTIONS (safe reads, CORS preflights)
		if c.Method() == fiber.MethodGet || c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		// 2. Bypass critical health checks
		if c.Path() == "/healthz" {
			return c.Next()
		}

		// 3. Bypass critical provider webhooks (billing, etc.)
		path := c.Path()
		for _, bp := range bypassPaths {
			if strings.HasPrefix(path, bp) {
				return c.Next()
			}
		}

		// 4. Check system overload BEFORE acquiring semaphore (H3 fix).
		// This avoids wasting a slot only to reject the request immediately.
		if limit.IsSystemOverloaded() {
			c.Set("Retry-After", "5")
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "The server is currently under heavy load. Please try again in a few seconds.",
				"code":  "SERVER_OVERLOADED",
			})
		}

		// 5. Try to acquire semaphore slot for mutating writes
		select {
		case mutatingSemaphore <- struct{}{}:
			defer func() { <-mutatingSemaphore }()
		default:
			c.Set("Retry-After", "3")
			return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "The server is currently experiencing extremely high concurrency. Please try again in a few seconds.",
				"code":  "CONCURRENCY_LIMIT_EXCEEDED",
			})
		}

		return c.Next()
	}
}
