package middlewares

import (
	"strings"
	"sync"

	"queuebuzz/internal/log"
	"queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

var (
	authService     *services.AuthService
	authInitOnce    sync.Once
	authInitialized bool
)

// InitAuthMiddleware sets the auth service for the middleware.
func InitAuthMiddleware(svc *services.AuthService) {
	authInitOnce.Do(func() {
		authService = svc
		authInitialized = true
	})
}

// AuthMiddleware validates the JWT from the Authorization header and attaches claims to Locals.
func AuthMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !authInitialized {
			log.Error().Msg("Auth middleware not initialized")
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		authHeader := c.Get(fiber.HeaderAuthorization)
		token, found := strings.CutPrefix(authHeader, "Bearer ")
		if !found || token == "" {
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

// OptionalAuthMiddleware tries to extract JWT claims but continues even without auth.
func OptionalAuthMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		if !authInitialized {
			return c.Next()
		}

		authHeader := c.Get(fiber.HeaderAuthorization)
		token, found := strings.CutPrefix(authHeader, "Bearer ")
		if !found || token == "" {
			return c.Next()
		}

		claims, err := authService.VerifyToken(token)
		if err != nil {
			return c.Next() // Continue without auth
		}

		c.Locals("claims", claims)
		c.Locals("role", claims.Role)
		c.Locals("host_id", claims.Subject)
		c.Locals("queue_id", claims.QueueID)
		c.Locals("raw_token", token)

		return c.Next()
	}
}
