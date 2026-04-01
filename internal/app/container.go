package app

import (
	"context"

	"queuebuzz/internal/config"
	"queuebuzz/internal/middlewares"
	authmodule "queuebuzz/internal/modules/auth"
	authhttp "queuebuzz/internal/modules/auth/http"
	authservice "queuebuzz/internal/modules/auth/service"
	customermodule "queuebuzz/internal/modules/customer"
	customerhttp "queuebuzz/internal/modules/customer/http"
	customerrepo "queuebuzz/internal/modules/customer/repository"
	customerservice "queuebuzz/internal/modules/customer/service"
	hostmodule "queuebuzz/internal/modules/host"
	hosthttp "queuebuzz/internal/modules/host/http"
	hostrepo "queuebuzz/internal/modules/host/repository"
	hostservice "queuebuzz/internal/modules/host/service"
	notificationmodule "queuebuzz/internal/modules/notification"
	notificationhttp "queuebuzz/internal/modules/notification/http"
	queuemodule "queuebuzz/internal/modules/queue"
	queuehttp "queuebuzz/internal/modules/queue/http"
	"queuebuzz/internal/modules/queue/jobs"
	queuerepo "queuebuzz/internal/modules/queue/repository"
	queueservice "queuebuzz/internal/modules/queue/service"
	"queuebuzz/internal/mongo"
	"queuebuzz/internal/redis"
	"queuebuzz/internal/services"
	"queuebuzz/internal/sse"

	redisdriver "github.com/redis/go-redis/v9"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

type Container struct {
	Config *config.Config
	Mongo  *mongodriver.Database
	Redis  *redisdriver.Client

	Auth         *authmodule.Module
	Host         *hostmodule.Module
	Queue        *queuemodule.Module
	Customer     *customermodule.Module
	Notification *notificationmodule.Module

	cancel context.CancelFunc
}

func NewContainer() *Container {
	cfg := config.Get()

	mongoDB := mongo.Connect(cfg.MongoURI, cfg.MongoDBName)
	rdb := redis.Connect(cfg.RedisURL, cfg.RedisPassword)

	queueCol := mongoDB.Collection("queues")
	entryCol := mongoDB.Collection("queue_entries")
	hostCol := mongoDB.Collection("hosts")

	redisSvc := services.NewRedisService(rdb)
	geoSvc := services.NewGeoService()
	joinCodeSvc := services.NewJoinCodeService(rdb)

	authSvc := authservice.NewAuthService(cfg.JWTPrivateKey, cfg.JWTPublicKey, redisSvc)
	socialAuthSvc := authservice.NewSocialAuthService(cfg, redisSvc)

	customerRedisRepo := customerrepo.NewRedisRepository(rdb)
	queueRedisRepo := queuerepo.NewRedisRepository(rdb)

	queueSvc := queueservice.New(queueCol, entryCol, queueRedisRepo)

	emailSvc := services.NewEmailService(cfg)
	otpSvc := services.NewOTPService(rdb)
	magicLinkSvc := services.NewMagicLinkService(rdb)
	notifSender := services.NewFirebaseSender(cfg.FirebaseCredentials)
	hostRepo := hostrepo.NewMongoRepository(hostCol, queueCol)
	hostSvc := hostservice.New(hostRepo)

	broker := sse.NewBroker()
	notifier := queueservice.NewQueueNotifier(broker)
	expirySvc := queueservice.NewExpiryService(rdb, queueCol, entryCol, queueRedisRepo, notifier)
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

	authHandler := authhttp.NewHandler(cfg, redisSvc, authSvc, socialAuthSvc, magicLinkSvc, otpSvc, emailSvc, hostSvc)
	hostHandler := hosthttp.NewHandler(cfg, authSvc, redisSvc, hostSvc, queueSvc)
	queueHandler := queuehttp.NewHandler(cfg, queueSvc, authSvc, queueRedisRepo, broker, notifier, posJob, hostNotifierJob, caller)
	customerSvc := customerservice.New(entryCol, customerRedisRepo, queueSvc)
	customerHandler := customerhttp.NewHandler(customerSvc, queueSvc, authSvc, joinCodeSvc, broker, posJob, hostNotifierJob)
	notifHandler := notificationhttp.NewHandler(queueSvc, notifSender)

	queueModule := queuemodule.New(queueHandler, notifHandler, expirySvc, posJob, hostNotifierJob)
	queueModule.Start(ctx)

	middlewares.InitAuthMiddleware(authSvc)
	middlewares.InitHostOwnerMiddleware(authSvc, redisSvc, queueCol)
	middlewares.InitGeoMiddleware(geoSvc)

	return &Container{
		Config:       cfg,
		Mongo:        mongoDB,
		Redis:        rdb,
		Auth:         authmodule.New(authHandler, authSvc),
		Host:         hostmodule.New(authHandler, hostHandler),
		Queue:        queueModule,
		Customer:     customermodule.New(customerHandler),
		Notification: notificationmodule.New(notifHandler),
		cancel:       cancel,
	}
}

func (c *Container) Shutdown() {
	if c.cancel != nil {
		c.cancel()
	}
	redis.Disconnect()
	mongo.Disconnect()
}
