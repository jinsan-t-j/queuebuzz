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

func (m *Module) Start(_ context.Context) {
}

func (m *Module) RegisterRoutes(router fiber.Router) {
	queue := router.Group("/queue")

	// Public / Guest Facing
	public := queue.Group("/p")
	public.Post("/create", m.QueueHandler.Create)
	public.Get("/:id/live", m.QueueHandler.GetLiveQueueByID)
	public.Post("/:id/join", m.QueueHandler.AddEntry)
	public.Get("/:id/events", m.QueueHandler.PublicEvents)

	// Host Management (Per-Queue)
	manage := queue.Group("/manage/:id", middlewares.HostAuthMiddleware(), middlewares.HostOwnerMiddleware())
	manage.Patch("/", m.QueueHandler.Update)
	manage.Get("/events", middlewares.SSERateLimiter, m.QueueHandler.StreamEvents)
	manage.Post("/call/:entry_id?", middlewares.HostActionLimiter, m.QueueHandler.CallEntry)
	manage.Post("/serve/:entry_id", middlewares.HostActionLimiter, m.QueueHandler.Serve)
	manage.Post("/pause", m.QueueHandler.PauseQueue)
	manage.Post("/resume", m.QueueHandler.ResumeQueue)
	manage.Post("/terminate", m.QueueHandler.TerminateQueue)
	manage.Post("/add-entry", m.QueueHandler.AddEntry)

	// Queue-specific history and broadcast
	manage.Get("/history", m.QueueHandler.GetHistory)
	manage.Post("/broadcast", m.NotificationHandler.BroadcastToQueue)

	// FCM and settings
	manage.Post("/register-host-fcm", m.QueueHandler.RegisterHostFCM)
	manage.Delete("/register-host-fcm", m.QueueHandler.UnregisterHostFCM)

	// Host Account Level (Global)
	host := queue.Group("/", middlewares.AuthMiddleware())
	host.Get("/dashboard", m.QueueHandler.GetDashboard)
	host.Get("/slug-check", m.QueueHandler.CheckSlug)
	host.Get("/history", m.QueueHandler.GetHistoryList)
	host.Get("/live", m.QueueHandler.GetLiveQueue)
}
