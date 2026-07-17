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
	billingprovider "queuebuzz/internal/modules/billing/provider"
	sentrywrap "queuebuzz/internal/sentry"
	"queuebuzz/internal/services/email"
	"queuebuzz/internal/validator"

	"github.com/dodopayments/dodopayments-go/option"
	gojson "github.com/goccy/go-json"
	swagger "github.com/gofiber/contrib/v3/swaggerui"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/idempotency"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

type App struct {
	Fiber     *fiber.App
	Container *Container
}

func New(cfg *config.Config, sender firebase.NotificationSender, dodoOpts ...option.RequestOption) *App {
	var emailProv email.Provider
	if cfg.IsProduction() {
		emailProv = &email.BrevoProvider{
			APIKey:    cfg.BrevoAPIKey,
			FromEmail: cfg.EmailFrom,
			FromName:  "QueueBuzz",
			ReplyTo:   cfg.EmailReplyTo,
		}
	} else {
		emailProv = &email.MailpitProvider{
			Host:      cfg.MailpitSMTPHost,
			Port:      cfg.MailpitSMTPPort,
			FromEmail: cfg.EmailFrom,
		}
	}

	billingProv := billingprovider.NewDodoProvider(cfg.DodoAPIKey, cfg.DodoWebhookKey, cfg.IsProduction(), dodoOpts...)

	container := NewContainer(cfg, sender, emailProv, billingProv)

	errHandler := middlewares.NewErrorHandler(cfg)

	app := fiber.New(fiber.Config{
		ErrorHandler:    errHandler.Handle,
		StructValidator: validator.New(),
		JSONEncoder:     gojson.Marshal,
		JSONDecoder:     gojson.Unmarshal,
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    15 * time.Second,
		IdleTimeout:     120 * time.Second,
		TrustProxy:      true,
		ProxyHeader:     "X-Forwarded-For",
		TrustProxyConfig: fiber.TrustProxyConfig{
			Loopback: true,
			Private:  true,
		},
	})

	app.Use(middlewares.SecurityHeaders())
	app.Use(middlewares.CORSMiddleware(cfg.AllowedOrigin))
	app.Use(middlewares.ResourceGuardMiddleware())
	app.Use(container.RateLimiters.Global)
	app.Use(recover.New(recover.Config{
		EnableStackTrace: true,
		StackTraceHandler: func(_ fiber.Ctx, e any) {
			sentrywrap.RecoverWithSentry(e)
		},
	}))
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

	// Start background jobs managed by the container
	a.Container.Start()

	if err := a.Fiber.Listen(":" + a.Container.Config.AppPort); err != nil {
		log.Fatal().Err(err).Msg("Failed to start server")
	}
}

func (a *App) Shutdown(ctx context.Context) error {
	log.Info().Msg("Shutting down gracefully...")

	// 1. Stop accepting new connections and drain in-flight HTTP requests
	//    (includes SSE streams — they see ctx cancellation and exit cleanly).
	if err := a.Fiber.ShutdownWithContext(ctx); err != nil {
		log.Error().Err(err).Msg("Fiber shutdown error")
	}

	// 2. Now that HTTP is drained, stop background jobs and SSE broker.
	a.Container.Shutdown()

	// 3. Flush Sentry before closing connections
	sentrywrap.Flush()

	// 4. Finally close database and redis connections
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
