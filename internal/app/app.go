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
	"github.com/gofiber/fiber/v3"
)

// App holds the Fiber instance plus all dependencies needed for the application lifecycle.
type App struct {
	fiber  *fiber.App
	config *config.Config
}

// New wires all dependencies, creates the Fiber app, registers middleware and routes.
func New() *App {
	cfg := config.Get()

	// --- Database connections ---
	mongoDB := db.ConnectMongo(cfg.MongoURI, cfg.MongoDBName)
	rdb := db.ConnectRedis(cfg.RedisURL, cfg.RedisPassword)

	// Collections
	queueCol := mongoDB.Collection("queues")
	entryCol := mongoDB.Collection("queue_entries")
	hostCol := mongoDB.Collection("hosts")

	// --- Services (dependency order) ---
	redisSvc := services.NewRedisService(rdb)
	geoSvc := services.NewGeoService()
	ticketSvc := services.NewTicketService(rdb)
	joinCodeSvc := services.NewJoinCodeService(rdb)
	authSvc := services.NewAuthService(cfg.JWTPrivateKey, cfg.JWTPublicKey, redisSvc)
	emailSvc := services.NewEmailService(cfg.ResendAPIKey, cfg.EmailFrom, cfg.EmailReplyTo)
	otpSvc := services.NewOTPService(rdb)
	magicLinkSvc := services.NewMagicLinkService(rdb)
	notifSender := services.NewFirebaseSender(cfg.FirebaseCredentials)
	queueSvc := services.NewQueueService(queueCol, entryCol, redisSvc, ticketSvc, joinCodeSvc, geoSvc)

	// --- WebSocket hub ---
	hub := ws.NewHub()

	// --- Queue expiry listener ---
	expiryListener := services.NewExpiryListener(rdb, queueCol, entryCol, redisSvc, hub, notifSender)
	ctx, cancel := context.WithCancel(context.Background())
	expiryListener.StartKeyspaceListener(ctx)
	go expiryListener.RunCronSweep(ctx)

	// --- Handlers ---
	hostHandler := handlers.NewHostHandler(authSvc, magicLinkSvc, otpSvc, emailSvc, redisSvc, hostCol, queueSvc)
	queueHandler := handlers.NewQueueHandler(queueSvc, authSvc)
	userHandler := handlers.NewUserHandler(queueSvc, redisSvc)
	notifHandler := handlers.NewNotificationHandler(queueSvc, notifSender)
	wsHandler := handlers.NewWebSocketHandler(authSvc, queueSvc, hub)

	// --- Init middleware dependencies ---
	middlewares.InitAuthMiddleware(authSvc)
	middlewares.InitHostOwnerMiddleware(authSvc, redisSvc, queueCol)
	middlewares.InitGeoMiddleware(geoSvc)

	// --- Fiber app ---
	errHandler := middlewares.NewErrorHandler(cfg)

	app := fiber.New(fiber.Config{
		ErrorHandler: errHandler.Handle,
		StructValidator: &validator.StructValidator{
			Validator: v10.New(),
		},
	})

	// --- Global middleware (order matters) ---
	app.Use(middlewares.SecurityHeaders())
	app.Use(middlewares.CORSMiddleware(cfg.AllowedOrigin))
	app.Use(middlewares.GlobalRateLimiter)

	// --- Routes ---
	routes.Register(app, &routes.Deps{
		HostHandler:         hostHandler,
		QueueHandler:        queueHandler,
		UserHandler:         userHandler,
		NotificationHandler: notifHandler,
		WebSocketHandler:    wsHandler,
	})

	// Store cancel func so Start() can invoke it on shutdown.
	// We stash it via a goroutine in Start() to keep App struct lean.
	go func() {
		<-ctx.Done() // will fire when cancel() is called in Start()
	}()

	// Keep cancel accessible for shutdown
	a := &App{fiber: app, config: cfg}
	a.startShutdownListener(cancel)
	return a
}

// Start begins listening and blocks until the process exits.
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

		// Close database connections
		db.DisconnectMongo()
		db.DisconnectRedis()

		log.Info().Msg("QueueBuzz server stopped")
		os.Exit(0)
	}()
}
