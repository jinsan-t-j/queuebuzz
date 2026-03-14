package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/db"
	"queuebuzz/internal/handlers"
	"queuebuzz/internal/log"
	"queuebuzz/internal/middlewares"
	"queuebuzz/internal/providers/validator"
	"queuebuzz/internal/routes"
	"queuebuzz/internal/services"
	"queuebuzz/internal/ws"

	v10 "github.com/go-playground/validator/v10"
	swagger "github.com/gofiber/contrib/v3/swaggerui"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/idempotency"
	"github.com/gofiber/fiber/v3/middleware/recover"
)

type App struct {
	fiber  *fiber.App
	config *config.Config
}

func New() *App {
	cfg := config.Get()

	mongoDB := db.ConnectMongo(cfg.MongoURI, cfg.MongoDBName)
	rdb := db.ConnectRedis(cfg.RedisURL, cfg.RedisPassword)

	queueCol := mongoDB.Collection("queues")
	entryCol := mongoDB.Collection("queue_entries")
	hostCol := mongoDB.Collection("hosts")

	redisSvc := services.NewRedisService(rdb)
	geoSvc := services.NewGeoService()
	ticketSvc := services.NewTicketService(rdb)
	joinCodeSvc := services.NewJoinCodeService(rdb)
	authSvc := services.NewAuthService(cfg.JWTPrivateKey, cfg.JWTPublicKey, redisSvc)
	emailSvc := services.NewEmailService(cfg)
	otpSvc := services.NewOTPService(rdb)
	magicLinkSvc := services.NewMagicLinkService(rdb)
	notifSender := services.NewFirebaseSender(cfg.FirebaseCredentials)
	queueSvc := services.NewQueueService(queueCol, entryCol, redisSvc, ticketSvc, joinCodeSvc, geoSvc)
	hostSvc := services.NewHostService(rdb, hostCol)

	hub := ws.NewHub()

	expiryListener := services.NewExpiryListener(rdb, queueCol, entryCol, redisSvc, hub, notifSender)
	ctx, cancel := context.WithCancel(context.Background())
	expiryListener.StartKeyspaceListener(ctx)
	go expiryListener.RunCronSweep(ctx)

	hostHandler := handlers.NewHostHandler(cfg, authSvc, magicLinkSvc, otpSvc, emailSvc, redisSvc, hostCol, hostSvc, queueSvc)
	queueHandler := handlers.NewQueueHandler(queueSvc, authSvc)
	userHandler := handlers.NewUserHandler(queueSvc, redisSvc)
	notifHandler := handlers.NewNotificationHandler(queueSvc, notifSender)
	wsHandler := handlers.NewWebSocketHandler(authSvc, queueSvc, hub)

	middlewares.InitAuthMiddleware(authSvc)
	middlewares.InitHostOwnerMiddleware(authSvc, redisSvc, queueCol)
	middlewares.InitGeoMiddleware(geoSvc)

	errHandler := middlewares.NewErrorHandler(cfg)

	app := fiber.New(fiber.Config{
		ErrorHandler: errHandler.Handle,
		StructValidator: &validator.StructValidator{
			Validator: v10.New(),
		},
	})

	app.Use(middlewares.SecurityHeaders())
	app.Use(middlewares.CORSMiddleware(cfg.AllowedOrigin))
	app.Use(middlewares.GlobalRateLimiter)
	app.Use(recover.New())
	app.Use(idempotency.New())

	routes.Register(app, &routes.Deps{
		HostHandler:         hostHandler,
		QueueHandler:        queueHandler,
		UserHandler:         userHandler,
		NotificationHandler: notifHandler,
		WebSocketHandler:    wsHandler,
	})

	app.Use(swagger.New(swagger.Config{
		BasePath: "/",
		Path:     "docs",
		FilePath: "./docs/swagger.json",
	}))

	app.Use(func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNotFound)
	})

	// Store cancel func so Start() can invoke it on shutdown.
	// We stash it via a goroutine in Start() to keep App struct lean.
	go func() {
		<-ctx.Done() // will fire when cancel() is called in Start()
	}()

	a := &App{fiber: app, config: cfg}
	a.startShutdownListener(cancel)
	return a
}

func (a *App) Start() {
	if err := a.fiber.Listen(":" + a.config.AppPort); err != nil {
		log.Fatal().Err(err).Msg("Failed to start server")
	}
}

// startShutdownListener spawns a goroutine that waits for SIGINT/SIGTERM,
// then drains Fiber and closes DB connections.
func (a *App) startShutdownListener(cancel context.CancelFunc) {
	go func() {
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		log.Info().Msg("Shutting down gracefully...")

		// Cancel context — stops expiry listener + cron sweep
		cancel()

		// Shutdown Fiber — drains active connections
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := a.fiber.ShutdownWithContext(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Server shutdown error")
		}

		db.DisconnectMongo()
		db.DisconnectRedis()

		log.Info().Msg("QueueBuzz server stopped")
		os.Exit(0)
	}()
}
