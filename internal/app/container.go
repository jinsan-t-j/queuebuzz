package app

import (
	"context"

	"queuebuzz/internal/log"
	"sync"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/firebase"
	"queuebuzz/internal/middlewares"
	authmodule "queuebuzz/internal/modules/auth"
	authhttp "queuebuzz/internal/modules/auth/http"
	authservice "queuebuzz/internal/modules/auth/service"
	billingmodule "queuebuzz/internal/modules/billing"
	billinghttp "queuebuzz/internal/modules/billing/http"
	billingjobs "queuebuzz/internal/modules/billing/jobs"
	billingprovider "queuebuzz/internal/modules/billing/provider"
	billingservice "queuebuzz/internal/modules/billing/service"
	customermodule "queuebuzz/internal/modules/customer"
	customerhttp "queuebuzz/internal/modules/customer/http"
	customerrepo "queuebuzz/internal/modules/customer/repository"
	customerservice "queuebuzz/internal/modules/customer/service"
	hostmodule "queuebuzz/internal/modules/host"
	hosthttp "queuebuzz/internal/modules/host/http"
	hostservice "queuebuzz/internal/modules/host/service"
	notificationmodule "queuebuzz/internal/modules/notification"
	notificationhttp "queuebuzz/internal/modules/notification/http"
	queuemodule "queuebuzz/internal/modules/queue"
	queuehttp "queuebuzz/internal/modules/queue/http"
	"queuebuzz/internal/modules/queue/jobs"
	queuerepo "queuebuzz/internal/modules/queue/repository"
	queueservice "queuebuzz/internal/modules/queue/service"
	systemmodule "queuebuzz/internal/modules/system"
	systemhttp "queuebuzz/internal/modules/system/http"
	systemservice "queuebuzz/internal/modules/system/service"
	"queuebuzz/internal/mongo"
	"queuebuzz/internal/redis"
	"queuebuzz/internal/services"
	"queuebuzz/internal/services/email"
	"queuebuzz/internal/services/storage"
	"queuebuzz/internal/sse"

	redisdriver "github.com/redis/go-redis/v9"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"

	redisstore "github.com/gofiber/storage/redis/v3"
)

type Container struct {
	Config       *config.Config
	MongoClient  *mongodriver.Client
	Mongo        *mongodriver.Database
	Redis        *redisdriver.Client
	RateLimiters *middlewares.RateLimiters

	Auth         *authmodule.Module
	Host         *hostmodule.Module
	Queue        *queuemodule.Module
	Customer     *customermodule.Module
	Notification *notificationmodule.Module
	Billing      *billingmodule.Module
	System       *systemmodule.Module
	Broker       *sse.Broker
	EmailSvc     *services.EmailService

	// Jobs
	posJob             *jobs.PositionJob
	hostNotifierJob    *jobs.HostNotifierJob
	caller             *jobs.Caller
	expiryJob          *jobs.ExpiryJob
	keyspaceJob        *jobs.KeyspaceJob
	notificationJob    *jobs.NotificationJob
	billingReminderJob *billingjobs.RenewalReminderJob

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewContainer(
	cfg *config.Config,
	notifSender firebase.NotificationSender,
	emailProv email.Provider,
	billingProv billingprovider.PaymentProvider,
) *Container {
	mClient, mongoDB := mongo.Connect(cfg)
	rdb := redis.Connect(cfg)

	redisStore := redisstore.New(redisstore.Config{
		URL:      cfg.RedisURL,
		PoolSize: cfg.RedisStorePoolSize,
	})
	limiters := middlewares.NewRateLimiters(cfg, redisStore)

	queueCol := mongoDB.Collection("queues")
	entryCol := mongoDB.Collection("queue_entries")
	hostCol := mongoDB.Collection("hosts")

	redisSvc := services.NewRedisService(rdb)
	authSvc := authservice.NewAuthService(cfg.JWTPrivateKey, cfg.JWTPublicKey, redisSvc)
	socialAuthSvc := authservice.NewSocialAuthService(cfg, redisSvc)

	customerRedisRepo := customerrepo.NewRedisRepository(rdb)
	queueRedisRepo := queuerepo.NewRedisRepository(rdb)

	systemSvc := systemservice.NewSystemService(cfg, mongoDB, rdb)
	emailSvc := services.NewEmailService(cfg, emailProv, systemSvc)
	recoverySvc := customerservice.NewRecoveryService(rdb, entryCol, emailSvc, cfg.RecoveryHMACSecret, cfg.FrontendURL)

	billingSvc := billingservice.NewBillingService(cfg, mongoDB, rdb, systemSvc, emailSvc, billingProv)
	billingHandler := billinghttp.NewHandler(cfg, billingSvc, billingProv, rdb)

	analyticsSvc := queueservice.NewAnalyticsService(queueCol, entryCol)
	queueSvc := queueservice.New(queueCol, entryCol, queueRedisRepo, analyticsSvc, billingSvc, systemSvc)
	otpSvc := services.NewOTPService(rdb)
	magicLinkSvc := services.NewMagicLinkService(rdb)
	hostSvc := hostservice.New(hostCol, queueCol, emailSvc)

	r2Svc, err := storage.NewR2Service(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize R2 storage")
	}

	ctx, cancel := context.WithCancel(context.Background())
	broker := sse.NewBroker(rdb)
	notifJob := jobs.NewNotificationJob(200, 3)
	notifier := queueservice.NewQueueNotifier(ctx, cfg, broker, notifSender, emailSvc, queueCol, entryCol, notifJob)
	expirySvc := queueservice.NewExpiryService(rdb, queueCol, entryCol, queueRedisRepo, notifier, billingSvc, systemSvc)
	posJob := jobs.NewPositionJob(cfg, expirySvc)
	hostNotifierJob := jobs.NewHostNotifierJob(cfg, queueSvc, expirySvc)
	caller := jobs.NewCaller(cfg, notifier)
	expiryJob := jobs.NewExpiryJob(expirySvc)
	keyspaceJob := jobs.NewKeyspaceJob(rdb, expirySvc)
	billingReminderJob := billingjobs.NewRenewalReminderJob(billingSvc)

	authHandler := authhttp.NewHandler(cfg, redisSvc, authSvc, socialAuthSvc, magicLinkSvc, otpSvc, emailSvc, hostSvc)
	hostHandler := hosthttp.NewHandler(cfg, authSvc, redisSvc, hostSvc, queueSvc, billingSvc, r2Svc, emailSvc)
	queueHandler := queuehttp.NewHandler(cfg, queueSvc, analyticsSvc, authSvc, hostSvc, queueRedisRepo, broker, notifier, posJob, hostNotifierJob, caller, billingSvc, emailSvc)
	customerSvc := customerservice.New(entryCol, customerRedisRepo, queueSvc)
	customerHandler := customerhttp.NewHandler(cfg, customerSvc, recoverySvc, queueSvc, authSvc, broker, posJob, hostNotifierJob, emailSvc)
	notifHandler := notificationhttp.NewHandler(queueSvc, notifSender)

	queueModule := queuemodule.New(cfg, queueHandler, notifHandler, expirySvc, posJob, hostNotifierJob, expiryJob, authSvc, queueCol)

	systemHandler := systemhttp.NewHandler(cfg, systemSvc, emailSvc)
	systemModule := systemmodule.New(systemHandler, cfg)

	billingModule := billingmodule.New(billingHandler, billingSvc, authSvc, billingReminderJob)

	return &Container{
		Config:       cfg,
		MongoClient:  mClient,
		Mongo:        mongoDB,
		Redis:        rdb,
		RateLimiters: limiters,
		Auth:         authmodule.New(authHandler, authSvc),
		Host:         hostmodule.New(authHandler, hostHandler, authSvc),
		Queue:        queueModule,
		Customer:     customermodule.New(customerHandler, authSvc),
		Notification: notificationmodule.New(notifHandler),
		Billing:      billingModule,
		System:       systemModule,
		Broker:       broker,
		EmailSvc:     emailSvc,

		posJob:             posJob,
		hostNotifierJob:    hostNotifierJob,
		caller:             caller,
		expiryJob:          expiryJob,
		keyspaceJob:        keyspaceJob,
		notificationJob:    notifJob,
		billingReminderJob: billingReminderJob,

		ctx:    ctx,
		cancel: cancel,
	}
}

func (c *Container) Start() {
	log.Info().Msg("Starting background jobs...")

	// Start SSE PubSub listener under lifecycle management
	if c.Broker != nil {
		c.Broker.StartPubSub(c.ctx)
	}

	c.runJob("PositionJob", c.posJob.Start)
	c.runJob("HostNotifierJob", c.hostNotifierJob.Start)
	c.runJob("CallerJob", c.caller.Start)
	c.runJob("ExpiryJob", c.expiryJob.Start)
	c.runJob("KeyspaceJob", c.keyspaceJob.Start)
	c.runJob("NotificationJob", c.notificationJob.Start)
	c.runJob("BillingReminderJob", c.billingReminderJob.Start)
	c.runJob("EmailJob", c.EmailSvc.Job().Start)
}

func (c *Container) runJob(name string, fn func(context.Context)) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("job", name).Msg("Background job panicked")
			}
		}()
		fn(c.ctx)
	}()
}

func (c *Container) Shutdown() {
	log.Info().Msg("Stopping background jobs...")
	if c.Broker != nil {
		c.Broker.Shutdown()
	}
	if c.cancel != nil {
		c.cancel()
	}

	// Wait for all background goroutines to finish
	c.wg.Wait()
	log.Info().Msg("All background jobs stopped")
}

func (c *Container) Cleanup() {
	if c.Redis != nil {
		if err := c.Redis.Close(); err != nil {
			log.Error().Err(err).Msg("Failed to disconnect from Redis")
		}
	}
	if c.MongoClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.MongoClient.Disconnect(ctx); err != nil {
			log.Error().Err(err).Msg("Failed to disconnect from MongoDB")
		}
	}
}
