package routes

import (
	"queuebuzz/internal/handlers"
	"queuebuzz/internal/middlewares"

	"github.com/gofiber/fiber/v3"
)

// Deps groups the handler instances required by the router.
type Deps struct {
	HostHandler         *handlers.HostHandler
	QueueHandler        *handlers.QueueHandler
	UserHandler         *handlers.UserHandler
	NotificationHandler *handlers.NotificationHandler
	WebSocketHandler    *handlers.WebSocketHandler
}

// Register mounts all API routes onto the Fiber app.
func Register(app *fiber.App, d *Deps) {
	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.SendString("OK")
	})

	app.Get("/auth/verify", middlewares.VerifyRateLimiter, d.HostHandler.Verify)

	api := app.Group("/api/v1")

	// ── Host Auth (public) ──────────────────────────────────────────
	host := api.Group("/host")
	host.Post("/register", middlewares.RegisterRateLimiter, d.HostHandler.Register)
	host.Post("/claim", middlewares.AuthMiddleware(), d.HostHandler.Claim)
	host.Get("/:public_id", d.HostHandler.GetProfile)
	host.Get("/:public_id/queues", d.HostHandler.GetQueues)

	// ── Queue (public) ──────────────────────────────────────────────
	queue := api.Group("/queue")
	queue.Post("/create", middlewares.OptionalAuthMiddleware(), d.QueueHandler.Create)
	queue.Get("/:id", d.QueueHandler.GetStatus)
	queue.Post("/join-by-code", middlewares.JoinRateLimiter, d.QueueHandler.JoinByCode)
	queue.Post("/:id/join", middlewares.JoinRateLimiter, d.QueueHandler.JoinByID)
	queue.Get("/:id/rejoin", d.UserHandler.Rejoin)
	queue.Post("/:id/heartbeat", d.QueueHandler.Heartbeat)

	// ── Queue — host-only (auth + ownership) ────────────────────────
	queueHost := queue.Group("/:id", middlewares.AuthMiddleware(), middlewares.HostOwnerMiddleware())
	queueHost.Post("/ping/:token", d.QueueHandler.PingUser)
	queueHost.Post("/next", d.QueueHandler.CallNext)
	queueHost.Delete("/user/:token", d.QueueHandler.RemoveUser)
	queueHost.Post("/close", d.QueueHandler.Close)
	queueHost.Post("/broadcast", d.NotificationHandler.BroadcastToQueue)

	// ── User — post-join (X-User-Token header) ──────────────────────
	queueUser := queue.Group("/:id/user")
	queueUser.Post("/email", d.UserHandler.AddEmail)
	queueUser.Post("/pin", d.UserHandler.SetPIN)

	// ── WebSocket ───────────────────────────────────────────────────
	app.Get("/ws/queue/:id", middlewares.WSRateLimiter, d.WebSocketHandler.Handle)
}
