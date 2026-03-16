package auth

import (
	"queuebuzz/internal/middlewares"
	authhttp "queuebuzz/internal/modules/auth/http"
	authservice "queuebuzz/internal/modules/auth/service"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	Handler *authhttp.Handler
	Service *authservice.AuthService
}

func New(handler *authhttp.Handler, service *authservice.AuthService) *Module {
	return &Module{Handler: handler, Service: service}
}

// RegisterRoutes registers the host authentication routes
// @Summary Register authentication routes
// @Description Register authentication routes
// @Tags auth
func (m *Module) RegisterRoutes(router fiber.Router) {
	router.Get("/auth/social/:provider/start", middlewares.RegisterRateLimiter, m.Handler.SocialLogin)
	router.Get("/auth/social/:provider/callback", middlewares.RegisterRateLimiter, m.Handler.SocialCallback)
	router.Post("/auth/social/:provider/callback", middlewares.RegisterRateLimiter, m.Handler.SocialCallback)
	router.Get("/auth/verify", middlewares.VerifyRateLimiter, m.Handler.Verify)

	router.Post("/api/v1/auth/register", middlewares.RegisterRateLimiter, m.Handler.Register)
	router.Post("/api/v1/auth/refresh/token", m.Handler.Refresh)
	router.Post("/api/v1/auth/logout", middlewares.AuthMiddleware(), m.Handler.Logout)
}
