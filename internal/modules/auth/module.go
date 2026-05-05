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

func (m *Module) RegisterRoutes(router fiber.Router, limiters *middlewares.RateLimiters) {
	router.Get("/auth/social/:provider/start", limiters.Register, m.Handler.SocialLogin)
	router.Get("/auth/social/:provider/callback", limiters.Register, m.Handler.SocialCallback)
	router.Post("/auth/social/:provider/callback", limiters.Register, m.Handler.SocialCallback)

	router.Get("/auth/verify", limiters.Verify, m.Handler.Verify)

	router.Post("/api/v1/auth/refresh/token", m.Handler.Refresh)
	router.Post("/api/v1/auth/login", limiters.Register, m.Handler.Authenticate)
	router.Post("/api/v1/auth/logout", middlewares.AuthMiddleware(m.Service), m.Handler.Logout)
	router.Post("/api/v1/system/email/refresh-blocklist", m.Handler.RefreshEmailBlocklist)
}
