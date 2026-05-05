package app

import (
	"context"

	"queuebuzz/internal/log"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/firebase"
	"queuebuzz/internal/middlewares"
	authmodule "queuebuzz/internal/modules/auth"
	authhttp "queuebuzz/internal/modules/auth/http"
	authservice "queuebuzz/internal/modules/auth/service"
	billingmodule "queuebuzz/internal/modules/billing"
	billinghttp "queuebuzz/internal/modules/billing/http"
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

	cancel context.CancelFunc
}

func NewContainer(cfg *config.Config, notifSender firebase.NotificationSender) *Container {
	mClient, mongoDB := mongo.Connect(cfg.DBUri, cfg.DBName)
	rdb := redis.Connect(cfg.RedisURL, cfg.RedisPassword)
	limiters := middlewares.NewRateLimiters(cfg)

	queueCol := mongoDB.Collection("queues")
	entryCol := mongoDB.Collection("queue_entries")
	hostCol := mongoDB.Collection("hosts")

	redisSvc := services.NewRedisService(rdb)
	joinCodeSvc := services.NewJoinCodeService(rdb)

	authSvc := authservice.NewAuthService(cfg.JWTPrivateKey, cfg.JWTPublicKey, redisSvc)
	socialAuthSvc := authservice.NewSocialAuthService(cfg, redisSvc)

	customerRedisRepo := customerrepo.NewRedisRepository(rdb)
	queueRedisRepo := queuerepo.NewRedisRepository(rdb)

	// Core services (order matters — later services depend on earlier ones)
	systemSvc := systemservice.NewSystemService(cfg, mongoDB, rdb)

	// Email provider + service
	var emailProvider email.Provider
	if cfg.IsProduction() {
		emailProvider = &email.BrevoProvider{
			APIKey:    cfg.BrevoAPIKey,
			FromEmail: cfg.EmailFrom,
			FromName:  "QueueBuzz",
			ReplyTo:   cfg.EmailReplyTo,
		}
	} else {
		emailProvider = &email.MailpitProvider{
			Host:      cfg.MailpitSMTPHost,
			Port:      cfg.MailpitSMTPPort,
			FromEmail: cfg.EmailFrom,
		}
	}
	emailSvc := services.NewEmailService(cfg, emailProvider, systemSvc)

	billingProv := billingprovider.NewDodoProvider(cfg.DodoAPIKey, cfg.DodoWebhookKey, !cfg.IsProduction())
	billingSvc := billingservice.NewBillingService(mongoDB, rdb, systemSvc, emailSvc, billingProv)
	billingHandler := billinghttp.NewHandler(billingSvc, billingProv, rdb)

	analyticsSvc := queueservice.NewAnalyticsService(queueCol, entryCol)
	queueSvc := queueservice.New(queueCol, entryCol, queueRedisRepo, analyticsSvc, billingSvc, systemSvc)
	otpSvc := services.NewOTPService(rdb)
	magicLinkSvc := services.NewMagicLinkService(rdb)
	hostSvc := hostservice.New(hostCol, queueCol, emailSvc)

	r2Svc, err := storage.NewR2Service(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize R2 storage")
	}

	broker := sse.NewBroker()
	notifier := queueservice.NewQueueNotifier(cfg, broker, notifSender, emailSvc, queueCol, entryCol)
	expirySvc := queueservice.NewExpiryService(rdb, queueCol, entryCol, queueRedisRepo, notifier, billingSvc, systemSvc)
	posJob := jobs.NewPositionJob(expirySvc)
	hostNotifierJob := jobs.NewHostNotifierJob(queueSvc, expirySvc)
	caller := jobs.NewCaller(notifier)
	expiryJob := jobs.NewExpiryJob(expirySvc)
	keyspaceJob := jobs.NewKeyspaceJob(rdb, expirySvc)
	ctx, cancel := context.WithCancel(context.Background())
	go posJob.Start(ctx)
	go hostNotifierJob.Start(ctx)
	go caller.Start(ctx)
	go expiryJob.Start(ctx)
	go keyspaceJob.Start(ctx)
	go emailSvc.Job().Start(ctx)

	authHandler := authhttp.NewHandler(cfg, redisSvc, authSvc, socialAuthSvc, magicLinkSvc, otpSvc, emailSvc, hostSvc)
	hostHandler := hosthttp.NewHandler(cfg, authSvc, redisSvc, hostSvc, queueSvc, r2Svc, emailSvc)
	queueHandler := queuehttp.NewHandler(cfg, queueSvc, analyticsSvc, authSvc, hostSvc, queueRedisRepo, broker, notifier, posJob, hostNotifierJob, caller, billingSvc, emailSvc)
	customerSvc := customerservice.New(entryCol, customerRedisRepo, queueSvc)
	customerHandler := customerhttp.NewHandler(cfg, customerSvc, queueSvc, authSvc, joinCodeSvc, broker, posJob, hostNotifierJob, emailSvc)
	notifHandler := notificationhttp.NewHandler(queueSvc, notifSender)

	queueModule := queuemodule.New(queueHandler, notifHandler, expirySvc, posJob, hostNotifierJob, authSvc, queueCol)
	queueModule.Start(ctx)

	systemHandler := systemhttp.NewHandler(cfg, systemSvc)
	systemModule := systemmodule.New(systemHandler)

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
		Billing:      billingmodule.New(billingHandler, billingSvc, authSvc),
		System:       systemModule,
		Broker:       broker,
		cancel:       cancel,
	}
}

func (c *Container) Shutdown() {
	if c.Broker != nil {
		c.Broker.Shutdown()
	}
	if c.cancel != nil {
		c.cancel()
	}
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
