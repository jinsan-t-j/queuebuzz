package middlewares

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

// SecurityHeaders adds security response headers to every request.
func SecurityHeaders() fiber.Handler {
	return func(c fiber.Ctx) error {
		host := c.Host()
		if idx := strings.Index(host, ":"); idx >= 0 {
			host = host[:idx]
		}

		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("X-Frame-Options", "DENY")
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			c.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			c.Set("Content-Security-Policy", "default-src 'self'")
		}
		c.Set("Referrer-Policy", "no-referrer")
		c.Set("X-Powered-By", "") // Remove entirely
		return c.Next()
	}
}
