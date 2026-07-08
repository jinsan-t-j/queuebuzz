package customer

import (
	"queuebuzz/internal/middlewares"
	authservice "queuebuzz/internal/modules/auth/service"
	customerhttp "queuebuzz/internal/modules/customer/http"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	Handler     *customerhttp.Handler
	AuthService *authservice.AuthService
}

func New(handler *customerhttp.Handler, authSvc *authservice.AuthService) *Module {
	return &Module{Handler: handler, AuthService: authSvc}
}

func (m *Module) RegisterRoutes(router fiber.Router, limiters *middlewares.RateLimiters) {
	customer := router.Group("/customer")

	entry := customer.Group("/entry")
	entry.Get("/", middlewares.CustomerAuthMiddleware(m.AuthService), m.Handler.GetEntry)
	entry.Get("/events", middlewares.CustomerAuthMiddleware(m.AuthService), m.Handler.StreamEvents)
	entry.Post("/leave", middlewares.CustomerAuthMiddleware(m.AuthService), m.Handler.Leave)

	entry.Get("/recover-by-token", limiters.Join, m.Handler.RecoverByToken)
	entry.Get("/recovery-token", middlewares.CustomerAuthMiddleware(m.AuthService), m.Handler.GetRecoveryToken)
	entry.Get("/recover-session", m.Handler.RecoverSession)
	entry.Post("/join/:id", limiters.Join, m.Handler.JoinByQueueID)
	entry.Post("/arrived", middlewares.CustomerAuthMiddleware(m.AuthService), m.Handler.ConfirmArrived)
	entry.Post("/confirm", middlewares.CustomerAuthMiddleware(m.AuthService), m.Handler.ConfirmStillHere)
	entry.Post("/finish", middlewares.CustomerAuthMiddleware(m.AuthService), m.Handler.FinishService)

	entry.Post("/update", middlewares.CustomerAuthMiddleware(m.AuthService), m.Handler.UpdateEntry)
}
