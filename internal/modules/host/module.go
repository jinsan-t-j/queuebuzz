package host

import (
	"queuebuzz/internal/middlewares"
	authhttp "queuebuzz/internal/modules/auth/http"
	authservice "queuebuzz/internal/modules/auth/service"
	hosthttp "queuebuzz/internal/modules/host/http"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	RegisterHandler *authhttp.Handler
	ClaimHandler    *hosthttp.Handler
	AuthService     *authservice.AuthService
}

func New(registerHandler *authhttp.Handler, hostHandler *hosthttp.Handler, authSvc *authservice.AuthService) *Module {
	return &Module{
		RegisterHandler: registerHandler,
		ClaimHandler:    hostHandler,
		AuthService:     authSvc,
	}
}

// RegisterRoutes registers the host routes
// @Summary Register host routes
// @Description Register host routes
// @Tags host
func (m *Module) RegisterRoutes(router fiber.Router) {
	host := router.Group("/host")
	host.Get("/me", middlewares.AuthMiddleware(m.AuthService), m.ClaimHandler.GetMe)
	host.Post("/claim", middlewares.AuthMiddleware(m.AuthService), m.ClaimHandler.Claim)
}
