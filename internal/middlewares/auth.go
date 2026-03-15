package middlewares

import (
	"queuebuzz/internal/log"
	authservice "queuebuzz/internal/modules/auth/service"

	"github.com/gofiber/fiber/v3"
)

var (
	authService     *authservice.AuthService
	authInitialized bool
)

func InitAuthMiddleware(svc *authservice.AuthService) {
	authService = svc
	authInitialized = true
}

func AuthMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !authInitialized {
			log.Error().Msg("Auth middleware not initialized")
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		token := c.Cookies("access_token")

		if token == "" {
			return fiber.NewError(fiber.StatusUnauthorized)
		}

		claims, err := authService.VerifyToken(token)
		if err != nil {
			return fiber.NewError(fiber.StatusUnauthorized)
		}

		c.Locals("claims", claims)
		c.Locals("role", claims.Role)
		c.Locals("host_id", claims.Subject)
		c.Locals("queue_id", claims.QueueID)
		c.Locals("raw_token", token)

		return c.Next()
	}
}

func OptionalAuthMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !authInitialized {
			return c.Next()
		}

		token := c.Cookies("access_token")
		if token == "" {
			return c.Next()
		}

		claims, err := authService.VerifyToken(token)
		if err != nil {
			return c.Next()
		}

		c.Locals("claims", claims)
		c.Locals("role", claims.Role)
		c.Locals("host_id", claims.Subject)
		c.Locals("queue_id", claims.QueueID)
		c.Locals("raw_token", token)

		return c.Next()
	}
}
