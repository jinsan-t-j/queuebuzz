package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"queuebuzz/internal/log"
	"queuebuzz/internal/middlewares"
	"queuebuzz/internal/providers/validator"

	swagger "github.com/gofiber/contrib/v3/swaggerui"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/idempotency"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

type App struct {
	fiber     *fiber.App
	container *Container
}

func New() *App {
	container := NewContainer()
	cfg := container.Config

	log.Info().Msg("Starting QueueBuzz server")

	errHandler := middlewares.NewErrorHandler(cfg)

	app := fiber.New(fiber.Config{
		ErrorHandler:    errHandler.Handle,
		StructValidator: validator.New(),
	})

	app.Use(middlewares.SecurityHeaders())
	app.Use(middlewares.CORSMiddleware(cfg.AllowedOrigin))
	app.Use(middlewares.GlobalRateLimiter)
	app.Use(recover.New())
	app.Use(idempotency.New())

	RegisterRoutes(app, container)

	if !cfg.IsProduction() {
		app.Use(swagger.New(swagger.Config{
			BasePath: "/",
			Path:     "docs",
			FilePath: "./docs/swagger.json",
		}))
	}

	app.Use(func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNotFound)
	})

	a := &App{fiber: app, container: container}
	a.startShutdownListener()
	return a
}

func (a *App) Start() {
	if err := a.fiber.Listen(":" + a.container.Config.AppPort); err != nil {
		log.Fatal().Err(err).Msg("Failed to start server")
	}

	log.Info().Msg("QueueBuzz server ready")
}

func (a *App) startShutdownListener() {
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		log.Info().Msg("Shutting down gracefully...")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := a.fiber.ShutdownWithContext(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Server shutdown error")
		}

		a.container.Shutdown()

		log.Info().Msg("QueueBuzz server stopped")
		os.Exit(0)
	}()
}
