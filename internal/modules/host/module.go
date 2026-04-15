package host

import (
	"queuebuzz/internal/middlewares"
	authhttp "queuebuzz/internal/modules/auth/http"
	hosthttp "queuebuzz/internal/modules/host/http"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	RegisterHandler *authhttp.Handler
	ClaimHandler    *hosthttp.Handler
}

func New(registerHandler *authhttp.Handler, hostHandler *hosthttp.Handler) *Module {
	return &Module{
		RegisterHandler: registerHandler,
		ClaimHandler:    hostHandler,
	}
}

// RegisterRoutes registers the host routes
// @Summary Register host routes
// @Description Register host routes
// @Tags host
func (m *Module) RegisterRoutes(router fiber.Router) {
	host := router.Group("/host")
	host.Get("/me", middlewares.AuthMiddleware(), m.ClaimHandler.GetMe)
	host.Post("/claim", middlewares.AuthMiddleware(), m.ClaimHandler.Claim)
	host.Get("/:public_id", m.ClaimHandler.GetProfile)
	host.Get("/:public_id/queues", m.ClaimHandler.GetQueues)
}
