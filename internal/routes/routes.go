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
	app.Get("/auth/social/:provider/start", d.HostHandler.SocialLogin)
	app.Get("/auth/social/:provider/callback", d.HostHandler.SocialCallback)
	app.Post("/auth/social/:provider/callback", d.HostHandler.SocialCallback)

	api := app.Group("/api/v1")

	host := api.Group("/host")
	host.Post("/register", middlewares.RegisterRateLimiter, d.HostHandler.Register)
	host.Get("/me", middlewares.AuthMiddleware(), d.HostHandler.GetMe)
	host.Post("/logout", d.HostHandler.Logout)
	host.Post("/claim", middlewares.AuthMiddleware(), d.HostHandler.Claim)
	host.Get("/:public_id", d.HostHandler.GetProfile)
	host.Get("/:public_id/queues", d.HostHandler.GetQueues)

	queue := api.Group("/queue")
	queue.Post("/create", middlewares.OptionalAuthMiddleware(), d.QueueHandler.Create)
	queue.Get("/:id", d.QueueHandler.GetStatus)
	queue.Post("/join-by-code", middlewares.JoinRateLimiter, d.QueueHandler.JoinByCode)
	queue.Post("/:id/join", middlewares.JoinRateLimiter, d.QueueHandler.JoinByID)
	queue.Get("/:id/rejoin", d.UserHandler.Rejoin)
	queue.Post("/:id/heartbeat", d.QueueHandler.Heartbeat)

	queueHost := queue.Group("/:id", middlewares.AuthMiddleware(), middlewares.HostOwnerMiddleware())
	queueHost.Post("/ping/:token", d.QueueHandler.PingUser)
	queueHost.Post("/next", d.QueueHandler.CallNext)
	queueHost.Delete("/user/:token", d.QueueHandler.RemoveUser)
	queueHost.Post("/close", d.QueueHandler.Close)
	queueHost.Post("/broadcast", d.NotificationHandler.BroadcastToQueue)

	queueUser := queue.Group("/:id/user")
	queueUser.Post("/email", d.UserHandler.AddEmail)
	queueUser.Post("/pin", d.UserHandler.SetPIN)

	app.Get("/ws/queue/:id", middlewares.WSRateLimiter, d.WebSocketHandler.Handle)
}
