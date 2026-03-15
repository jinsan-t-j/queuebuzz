package app

import "github.com/gofiber/fiber/v3"

func RegisterRoutes(app *fiber.App, c *Container) {
	app.Get("/healthz", func(ctx fiber.Ctx) error {
		return ctx.SendString("OK")
	})

	c.Auth.RegisterRoutes(app)

	api := app.Group("/api/v1")
	c.Host.RegisterRoutes(api)
	c.Queue.RegisterRoutes(api)
	c.Customer.RegisterRoutes(api)
	c.Realtime.RegisterRoutes(app)
}
