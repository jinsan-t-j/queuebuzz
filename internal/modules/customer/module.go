package customer

import (
	customerhttp "queuebuzz/internal/modules/customer/http"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	Handler *customerhttp.Handler
}

func New(handler *customerhttp.Handler) *Module {
	return &Module{Handler: handler}
}

func (m *Module) RegisterRoutes(router fiber.Router) {
	entry := router.Group("/entry")
	entry.Get("/:id/rejoin", m.Handler.Rejoin)
	entry.Get("/:id/status", m.Handler.GetStatus)
	entry.Get("/:id/events", m.Handler.StreamEvents)

	entryUser := entry.Group("/:id/user")
	entryUser.Post("/email", m.Handler.AddEmail)
	entryUser.Post("/pin", m.Handler.SetPIN)
}
