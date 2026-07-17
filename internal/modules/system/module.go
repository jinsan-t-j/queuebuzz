package system

import (
	"queuebuzz/internal/config"
	"queuebuzz/internal/middlewares"
	"queuebuzz/internal/modules/system/http"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	handler *http.Handler
	cfg     *config.Config
}

func New(handler *http.Handler, cfg *config.Config) *Module {
	return &Module{
		handler: handler,
		cfg:     cfg,
	}
}

func (m *Module) RegisterRoutes(router fiber.Router, limiters *middlewares.RateLimiters) {
	group := router.Group("/system")

	systemAuth := middlewares.SystemSecretMiddleware(m.cfg)
	group.Get("/settings", systemAuth, m.handler.GetSettings)
	group.Patch("/settings", systemAuth, m.handler.UpdateSettings)

	router.Post("/support", limiters.Lenient, m.handler.SubmitSupport)
}
