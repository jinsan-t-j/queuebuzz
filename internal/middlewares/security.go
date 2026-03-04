package middlewares

import (
	"github.com/gofiber/fiber/v3"
)

// SecurityHeaders adds security response headers to every request.
func SecurityHeaders() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		c.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		c.Set("Content-Security-Policy", "default-src 'self'")
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("X-Powered-By", "") // Remove entirely
		return c.Next()
	}
}
