package queue

import (
	"queuebuzz/internal/middlewares"
	notificationhttp "queuebuzz/internal/modules/notification/http"
	queuehttp "queuebuzz/internal/modules/queue/http"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	QueueHandler        *queuehttp.Handler
	NotificationHandler *notificationhttp.Handler
}

func New(queueHandler *queuehttp.Handler, notificationHandler *notificationhttp.Handler) *Module {
	return &Module{
		QueueHandler:        queueHandler,
		NotificationHandler: notificationHandler,
	}
}

func (m *Module) RegisterRoutes(router fiber.Router) {
	queue := router.Group("/queue")
	queue.Post("/create", middlewares.OptionalAuthMiddleware(), m.QueueHandler.Create)
	queue.Get("/live", middlewares.AuthMiddleware(), m.QueueHandler.GetLiveQueue)
	queue.Get("/slug-check", m.QueueHandler.CheckSlug)
	queue.Get("/:id", m.QueueHandler.GetStatus)
	queue.Post("/join-by-code", middlewares.JoinRateLimiter, m.QueueHandler.JoinByCode)
	queue.Post("/:id/join", middlewares.JoinRateLimiter, m.QueueHandler.JoinByID)
	queue.Post("/:id/heartbeat", m.QueueHandler.Heartbeat)

	queueHost := queue.Group("/:id", middlewares.OptionalAuthMiddleware(), middlewares.HostOwnerMiddleware())
	queueHost.Get("/live", m.QueueHandler.GetLiveQueueByID)
	queueHost.Post("/ping/:token", m.QueueHandler.PingUser)
	queueHost.Post("/next", m.QueueHandler.CallNext)
	queueHost.Post("/pause", m.QueueHandler.PauseQueue)
	queueHost.Post("/resume", m.QueueHandler.ResumeQueue)
	queueHost.Post("/terminate", m.QueueHandler.TerminateQueue)
	queueHost.Get("/history", m.QueueHandler.GetHistory)
	queueHost.Post("/broadcast", m.NotificationHandler.BroadcastToQueue)
}
