package realtime

import (
	"queuebuzz/internal/middlewares"
	realtimehttp "queuebuzz/internal/modules/realtime/http"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	Handler *realtimehttp.Handler
}

func New(handler *realtimehttp.Handler) *Module {
	return &Module{Handler: handler}
}

func (m *Module) RegisterRoutes(router fiber.Router) {
	router.Get("/ws/queue/:id", middlewares.WSRateLimiter, m.Handler.Handle)
}
