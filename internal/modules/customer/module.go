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
	customer := router.Group("/customer")

	entry := customer.Group("/entry")
	entry.Get("/", middlewares.CustomerAuthMiddleware(), m.Handler.GetEntry)
	entry.Get("/events", middlewares.CustomerAuthMiddleware(), m.Handler.StreamEvents)
	entry.Post("/leave", middlewares.CustomerAuthMiddleware(), m.Handler.Leave)

	entry.Post("/join-by-code", middlewares.JoinRateLimiter, m.Handler.JoinByCode)
	entry.Get("/resolve-code/:code", middlewares.JoinRateLimiter, m.Handler.ResolveCode)
	entry.Get("/recover-session", m.Handler.RecoverSession)
	entry.Post("/join/:id", middlewares.JoinRateLimiter, m.Handler.JoinByQueueID)
	entry.Post("/confirm", middlewares.CustomerAuthMiddleware(), m.Handler.Heartbeat)
	entry.Post("/arrived", middlewares.CustomerAuthMiddleware(), m.Handler.ConfirmArrived)

	entry.Post("/update", middlewares.CustomerAuthMiddleware(), m.Handler.UpdateEntry)
}
