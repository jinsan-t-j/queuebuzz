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

func setAuthLocals(c fiber.Ctx, claims *authservice.QueueBuzzClaims, token string) {
	c.Locals("claims", claims)
	c.Locals("role", claims.Role)
	c.Locals("host_id", claims.Subject)
	c.Locals("host_public_id", claims.PublicID)
	c.Locals("queue_id", claims.QueueID)
	c.Locals("raw_access_token", token)
}

func AuthMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !authInitialized {
			log.Error().Msg("Auth middleware not initialized")
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		token := c.Cookies("access_token")
		if token == "" {
			token = c.Cookies("queuebuzz_host_token")
		}

		if token == "" {
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		claims, err := authService.VerifyToken(token)
		if err != nil {
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		setAuthLocals(c, claims, token)

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
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		setAuthLocals(c, claims, token)

		return c.Next()
	}
}

func HostAuthMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !authInitialized {
			return c.Next()
		}

		token := c.Cookies("access_token")
		if token == "" {
			token = c.Cookies("queuebuzz_host_token")
		}

		if token == "" {
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		claims, err := authService.VerifyToken(token)
		if err != nil {
			return c.SendStatus(fiber.StatusUnauthorized)
		}

		setAuthLocals(c, claims, token)

		return c.Next()
	}
}
