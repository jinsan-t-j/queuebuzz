package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/firebase"
	"queuebuzz/internal/log"
	"queuebuzz/internal/middlewares"
	"queuebuzz/internal/validator"

	swagger "github.com/gofiber/contrib/v3/swaggerui"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/idempotency"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

type App struct {
	Fiber     *fiber.App
	Container *Container
}

func New(cfg *config.Config, sender firebase.NotificationSender) *App {
	container := NewContainer(cfg, sender)

	errHandler := middlewares.NewErrorHandler(cfg)

	app := fiber.New(fiber.Config{
		ErrorHandler:    errHandler.Handle,
		StructValidator: validator.New(),
	})

	app.Use(middlewares.SecurityHeaders())
	app.Use(middlewares.CORSMiddleware(cfg.AllowedOrigin))
	app.Use(container.RateLimiters.Global)
	app.Use(recover.New())
	app.Use(idempotency.New())

	RegisterRoutes(app, container)

	if !cfg.IsProduction() && !cfg.IsTesting() {
		swaggerPath := "./docs/swagger.json"
		if _, err := os.Stat(swaggerPath); err == nil {
			app.Use(swagger.New(swagger.Config{
				BasePath: "/",
				Path:     "docs",
				FilePath: swaggerPath,
			}))
		}
	}

	app.Use(func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNotFound)
	})

	return &App{Fiber: app, Container: container}
}

func (a *App) Start() {
	go a.listenForShutdown()

	if err := a.Fiber.Listen(":" + a.Container.Config.AppPort); err != nil {
		log.Fatal().Err(err).Msg("Failed to start server")
	}
}

func (a *App) Shutdown(ctx context.Context) error {
	log.Info().Msg("Shutting down gracefully...")

	// 1. Stop background tasks and kill long-running SSE streams
	a.Container.Shutdown()

	// 2. Stop Fiber (waits for active regular HTTP requests)
	if err := a.Fiber.ShutdownWithContext(ctx); err != nil {
		log.Error().Err(err).Msg("Fiber shutdown error")
		return err
	}

	// 3. Finally close database and redis connections
	a.Container.Cleanup()

	log.Info().Msg("QueueBuzz server stopped")
	return nil
}

func (a *App) listenForShutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()

	if err := a.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}
}
