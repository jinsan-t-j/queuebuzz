package system

import (
	"queuebuzz/internal/modules/system/http"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	handler *http.Handler
}

func New(handler *http.Handler) *Module {
	return &Module{
		handler: handler,
	}
}

func (m *Module) RegisterRoutes(router fiber.Router) {
	group := router.Group("/system")
	group.Get("/settings", m.handler.GetSettings)
	group.Patch("/settings", m.handler.UpdateSettings)
}
