package queue

import (
	"queuebuzz/internal/middlewares"
	authservice "queuebuzz/internal/modules/auth/service"
	notificationhttp "queuebuzz/internal/modules/notification/http"
	queuehttp "queuebuzz/internal/modules/queue/http"
	"queuebuzz/internal/modules/queue/jobs"
	queueservice "queuebuzz/internal/modules/queue/service"

	"github.com/gofiber/fiber/v3"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Module struct {
	QueueHandler        *queuehttp.Handler
	NotificationHandler *notificationhttp.Handler
	expiryService       *queueservice.ExpiryService
	posJob              *jobs.PositionJob
	hostNotifierJob     *jobs.HostNotifierJob
	ExpiryJob           *jobs.ExpiryJob
	authService         *authservice.AuthService
	queueCol            *mongo.Collection
}

func New(
	queueHandler *queuehttp.Handler,
	notificationHandler *notificationhttp.Handler,
	expiryService *queueservice.ExpiryService,
	posJob *jobs.PositionJob,
	hostNotifierJob *jobs.HostNotifierJob,
	expiryJob *jobs.ExpiryJob,
	authService *authservice.AuthService,
	queueCol *mongo.Collection,
) *Module {
	return &Module{
		QueueHandler:        queueHandler,
		NotificationHandler: notificationHandler,
		expiryService:       expiryService,
		posJob:              posJob,
		hostNotifierJob:     hostNotifierJob,
		ExpiryJob:           expiryJob,
		authService:         authService,
		queueCol:            queueCol,
	}
}

func (m *Module) RegisterRoutes(router fiber.Router, limiters *middlewares.RateLimiters) {
	queue := router.Group("/queue")

	// Public / Guest Facing
	public := queue.Group("/p")
	public.Get("/find", limiters.Join, m.QueueHandler.FindQueue)
	public.Post("/create", middlewares.OptionalAuthMiddleware(m.authService), queuehttp.CreateQueueGuard(m.QueueHandler.BillingSvc, m.QueueHandler.Service), m.QueueHandler.Create)
	public.Get("/:id/live", m.QueueHandler.GetLiveQueueByID)
	public.Post("/:id/join", queuehttp.GuestCapacityGuard(m.QueueHandler.BillingSvc, m.QueueHandler.Service), m.QueueHandler.AddEntry)
	public.Get("/:id/events", m.QueueHandler.PublicEvents)

	// Host Management (Per-Queue)
	manage := queue.Group("/manage/:id", middlewares.HostAuthMiddleware(m.authService), middlewares.HostOwnerMiddleware(m.authService, m.queueCol))
	manage.Patch("/", m.QueueHandler.Update)
	manage.Get("/events", limiters.SSE, m.QueueHandler.StreamEvents)
	manage.Post("/call/:entry_id?", limiters.Host, m.QueueHandler.CallEntry)
	manage.Post("/serve/:entry_id", limiters.Host, m.QueueHandler.Serve)
	manage.Post("/pause", m.QueueHandler.PauseQueue)
	manage.Post("/resume", m.QueueHandler.ResumeQueue)
	manage.Post("/terminate", m.QueueHandler.TerminateQueue)
	manage.Post("/add-entry", queuehttp.GuestCapacityGuard(m.QueueHandler.BillingSvc, m.QueueHandler.Service), m.QueueHandler.AddEntry)

	// Queue-specific history and broadcast
	manage.Get("/history", queuehttp.HistoryAccessGuard(m.QueueHandler.BillingSvc), m.QueueHandler.GetHistory)
	manage.Post("/broadcast", m.NotificationHandler.BroadcastToQueue)

	// FCM and settings
	manage.Post("/register-host-fcm", m.QueueHandler.RegisterHostFCM)
	manage.Delete("/register-host-fcm", m.QueueHandler.UnregisterHostFCM)

	// Host Account Level (Global)
	host := queue.Group("/", middlewares.AuthMiddleware(m.authService))
	host.Get("/dashboard", m.QueueHandler.GetDashboard)
	host.Get("/slug-check", m.QueueHandler.CheckSlug)
	host.Get("/history", m.QueueHandler.GetHistoryList)
	host.Delete("/history", m.QueueHandler.DeleteHistoryBulk)
	host.Delete("/history/:id", m.QueueHandler.DeleteHistory)
	host.Post("/history/clear", m.QueueHandler.ClearHistory)
	host.Get("/live", m.QueueHandler.GetLiveQueue)
}
