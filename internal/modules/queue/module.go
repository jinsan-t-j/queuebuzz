package queue

import (
	"context"

	"queuebuzz/internal/middlewares"
	notificationhttp "queuebuzz/internal/modules/notification/http"
	queuehttp "queuebuzz/internal/modules/queue/http"
	"queuebuzz/internal/modules/queue/jobs"
	queueservice "queuebuzz/internal/modules/queue/service"

	"github.com/gofiber/fiber/v3"
)

type Module struct {
	QueueHandler        *queuehttp.Handler
	NotificationHandler *notificationhttp.Handler
	expiryService       *queueservice.ExpiryService
	posJob              *jobs.PositionJob
	hostNotifierJob     *jobs.HostNotifierJob
}

func New(
	queueHandler *queuehttp.Handler,
	notificationHandler *notificationhttp.Handler,
	expiryService *queueservice.ExpiryService,
	posJob *jobs.PositionJob,
	hostNotifierJob *jobs.HostNotifierJob,
) *Module {
	return &Module{
		QueueHandler:        queueHandler,
		NotificationHandler: notificationHandler,
		expiryService:       expiryService,
		posJob:              posJob,
		hostNotifierJob:     hostNotifierJob,
	}
}

func (m *Module) Start(ctx context.Context) {
}

func (m *Module) RegisterRoutes(router fiber.Router) {
	queue := router.Group("/queue")
	queue.Post("/create", middlewares.OptionalAuthMiddleware(), m.QueueHandler.Create)
	queue.Get("/live", middlewares.AuthMiddleware(), m.QueueHandler.GetLiveQueue)

	queue.Get("/slug-check", m.QueueHandler.CheckSlug)

	queue.Post("/:id/join", m.QueueHandler.AddEntry)

	queue.Get("/:id/live", m.QueueHandler.GetLiveQueueByID)
	queue.Get("/:id/events/public", m.QueueHandler.PublicEvents)

	queueHost := queue.Group("/:id", middlewares.HostAuthMiddleware(), middlewares.HostOwnerMiddleware())
	queueHost.Patch("/", m.QueueHandler.Update)
	queueHost.Get("/events", middlewares.SSERateLimiter, m.QueueHandler.StreamEvents)
	queueHost.Post("/call/:entry_id?", middlewares.HostActionLimiter, m.QueueHandler.CallEntry)
	queueHost.Post("/serve/:entry_id", middlewares.HostActionLimiter, m.QueueHandler.Serve)
	queueHost.Post("/pause", m.QueueHandler.PauseQueue)
	queueHost.Post("/resume", m.QueueHandler.ResumeQueue)
	queueHost.Post("/terminate", m.QueueHandler.TerminateQueue)
	queueHost.Post("/add-entry", m.QueueHandler.AddEntry)

	queueHost.Get("/history", m.QueueHandler.GetHistory)
	queueHost.Post("/broadcast", m.NotificationHandler.BroadcastToQueue)
}
