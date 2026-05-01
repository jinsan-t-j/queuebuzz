package middlewares

import (
	"queuebuzz/internal/config"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// SystemSecretMiddleware protects routes with the SYSTEM_API_SECRET.
// It expects the secret in the Authorization header as a Bearer token.
func SystemSecretMiddleware(cfg *config.Config) fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return fiber.NewError(fiber.StatusUnauthorized, "Missing authorization header")
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			return fiber.NewError(fiber.StatusUnauthorized, "Invalid authorization header format")
		}

		if parts[1] != cfg.SystemAPISecret {
			return fiber.NewError(fiber.StatusUnauthorized, "Invalid system API secret")
		}

		return c.Next()
	}
}
