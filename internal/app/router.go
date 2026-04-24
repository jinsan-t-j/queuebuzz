package app

import "github.com/gofiber/fiber/v3"

func RegisterRoutes(app *fiber.App, c *Container) {
	app.Get("/healthz", func(ctx fiber.Ctx) error {
		return ctx.SendString("OK")
	})

	c.Auth.RegisterRoutes(app, c.RateLimiters)

	api := app.Group("/api/v1")
	c.Host.RegisterRoutes(api)
	c.Queue.RegisterRoutes(api, c.RateLimiters)
	c.Customer.RegisterRoutes(api, c.RateLimiters)
}
