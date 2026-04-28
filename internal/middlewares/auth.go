package middlewares

import (
	authservice "queuebuzz/internal/modules/auth/service"

	"github.com/gofiber/fiber/v3"
)

func setAuthLocals(c fiber.Ctx, claims *authservice.QueueBuzzClaims, token string) {
	c.Locals("claims", claims)
	c.Locals("role", claims.Role)
	c.Locals("host_id", claims.Subject)
	c.Locals("host_public_id", claims.PublicID)
	c.Locals("queue_id", claims.QueueID)
	c.Locals("entry_id", claims.EntryID)
	c.Locals("raw_access_token", token)
}

func AuthMiddleware(authService *authservice.AuthService) fiber.Handler {
	return func(c fiber.Ctx) error {
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

func CustomerAuthMiddleware(authService *authservice.AuthService) fiber.Handler {
	return func(c fiber.Ctx) error {
		token := c.Cookies("guest_entry_token")
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

func OptionalCustomerAuthMiddleware(authService *authservice.AuthService) fiber.Handler {
	return func(c fiber.Ctx) error {
		token := c.Cookies("guest_entry_token")
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

func OptionalAuthMiddleware(authService *authservice.AuthService) fiber.Handler {
	return func(c fiber.Ctx) error {
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

func HostAuthMiddleware(authService *authservice.AuthService) fiber.Handler {
	return func(c fiber.Ctx) error {
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
