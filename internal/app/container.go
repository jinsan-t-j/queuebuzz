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
	hostmodule "queuebuzz/internal/modules/host"
	hosthttp "queuebuzz/internal/modules/host/http"
	hostrepo "queuebuzz/internal/modules/host/repository"
	hostservice "queuebuzz/internal/modules/host/service"
	notificationmodule "queuebuzz/internal/modules/notification"
	notificationhttp "queuebuzz/internal/modules/notification/http"
	queuemodule "queuebuzz/internal/modules/queue"
	queuehttp "queuebuzz/internal/modules/queue/http"
	queueservice "queuebuzz/internal/modules/queue/service"
	realtimemodule "queuebuzz/internal/modules/realtime"
	realtimehttp "queuebuzz/internal/modules/realtime/http"
	"queuebuzz/internal/mongo"
	"queuebuzz/internal/redis"
	"queuebuzz/internal/services"
	"queuebuzz/internal/ws"

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
	Realtime     *realtimemodule.Module

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
	ticketSvc := services.NewTicketService(rdb)
	joinCodeSvc := services.NewJoinCodeService(rdb)
	authSvc := authservice.NewAuthService(cfg.JWTPrivateKey, cfg.JWTPublicKey, redisSvc)
	socialAuthSvc := authservice.NewSocialAuthService(cfg, redisSvc)
	emailSvc := services.NewEmailService(cfg)
	otpSvc := services.NewOTPService(rdb)
	magicLinkSvc := services.NewMagicLinkService(rdb)
	notifSender := services.NewFirebaseSender(cfg.FirebaseCredentials)
	queueSvc := queueservice.New(queueCol, entryCol, redisSvc, ticketSvc, joinCodeSvc, geoSvc)
	hostRepo := hostrepo.NewMongoRepository(hostCol, queueCol)
	hostSvc := hostservice.New(hostRepo)

	hub := ws.NewHub()

	expiryListener := services.NewExpiryListener(rdb, queueCol, entryCol, redisSvc, hub, notifSender)
	ctx, cancel := context.WithCancel(context.Background())
	expiryListener.StartKeyspaceListener(ctx)
	go expiryListener.RunCronSweep(ctx)

	authHandler := authhttp.NewHandler(cfg, authSvc, socialAuthSvc, magicLinkSvc, otpSvc, emailSvc, hostSvc)
	hostHandler := hosthttp.NewHandler(cfg, authSvc, redisSvc, hostSvc, queueSvc)
	queueHandler := queuehttp.NewHandler(queueSvc, authSvc, joinCodeSvc, redisSvc)
	customerHandler := customerhttp.NewHandler(queueSvc, redisSvc)
	notifHandler := notificationhttp.NewHandler(queueSvc, notifSender)
	wsHandler := realtimehttp.NewHandler(authSvc, queueSvc, hub)

	middlewares.InitAuthMiddleware(authSvc)
	middlewares.InitHostOwnerMiddleware(authSvc, redisSvc, queueCol)
	middlewares.InitGeoMiddleware(geoSvc)

	return &Container{
		Config:       cfg,
		Mongo:        mongoDB,
		Redis:        rdb,
		Auth:         authmodule.New(authHandler, authSvc),
		Host:         hostmodule.New(authHandler, hostHandler),
		Queue:        queuemodule.New(queueHandler, notifHandler),
		Customer:     customermodule.New(customerHandler),
		Notification: notificationmodule.New(notifHandler),
		Realtime:     realtimemodule.New(wsHandler),
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
