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
	queue := router.Group("/queue")
	queue.Get("/:id/rejoin", m.Handler.Rejoin)

	queueUser := queue.Group("/:id/user")
	queueUser.Post("/email", m.Handler.AddEmail)
	queueUser.Post("/pin", m.Handler.SetPIN)
}
