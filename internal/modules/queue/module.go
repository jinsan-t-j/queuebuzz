package queue

import (
	"context"

	"queuebuzz/internal/middlewares"
	notificationhttp "queuebuzz/internal/modules/notification/http"
	queuehttp "queuebuzz/internal/modules/queue/http"
	queueservice "queuebuzz/internal/modules/queue/service"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	QueueHandler        *queuehttp.Handler
	NotificationHandler *notificationhttp.Handler
	expiryService       *queueservice.ExpiryService
}

func New(
	queueHandler *queuehttp.Handler,
	notificationHandler *notificationhttp.Handler,
	expiryService *queueservice.ExpiryService,
) *Module {
	return &Module{
		QueueHandler:        queueHandler,
		NotificationHandler: notificationHandler,
		expiryService:       expiryService,
	}
}

func (m *Module) Start(ctx context.Context) {
	if m.expiryService == nil {
		return
	}

	m.expiryService.StartKeyspaceListener(ctx)
	go m.expiryService.RunCronSweep(ctx)
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

	// SSE stream — authenticated host only

	queueHost := queue.Group("/:id", middlewares.HostAuthMiddleware(), middlewares.HostOwnerMiddleware())
	queueHost.Get("/live", m.QueueHandler.GetLiveQueueByID)
	queueHost.Get("/events", middlewares.SSERateLimiter, m.QueueHandler.Events)
	queueHost.Post("/ping/:token", m.QueueHandler.PingUser)
	queueHost.Post("/next", m.QueueHandler.CallNext)
	queueHost.Post("/pause", m.QueueHandler.PauseQueue)
	queueHost.Post("/resume", m.QueueHandler.ResumeQueue)
	queueHost.Post("/terminate", m.QueueHandler.TerminateQueue)
	queueHost.Post("/add-entry", m.QueueHandler.AddEntry)
	queueHost.Get("/history", m.QueueHandler.GetHistory)
	queueHost.Post("/broadcast", m.NotificationHandler.BroadcastToQueue)
}
