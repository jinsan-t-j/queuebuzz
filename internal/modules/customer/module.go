package customer

import (
	"queuebuzz/internal/middlewares"
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
	entry.Get("/rejoin", middlewares.OptionalCustomerAuthMiddleware(), m.Handler.Rejoin)
	entry.Get("/", middlewares.CustomerAuthMiddleware(), m.Handler.GetEntry)
	entry.Get("/events", middlewares.CustomerAuthMiddleware(), m.Handler.StreamEvents)

	entryUser := entry.Group("/user", middlewares.CustomerAuthMiddleware())
	entryUser.Post("/email", m.Handler.AddEmail)
	entryUser.Post("/pin", m.Handler.SetPIN)
}
