package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"queuebuzz/internal/app"
	"queuebuzz/internal/config"
	"queuebuzz/internal/database/seed"
	"queuebuzz/internal/firebase"
	"queuebuzz/internal/log"
	"queuebuzz/internal/mongo"

	_ "queuebuzz/docs"
)

// @title           QueueBuzz API
// @version         1.0
// @description     Virtual queue management platform API.
// @termsOfService  https://queuebuzz.com/terms

// @contact.name   QueueBuzz Support
// @contact.email  support@queuebuzz.com

// @license.name  Proprietary

// @host      localhost:8080
// @BasePath  /api

// @securityDefinitions.apikey  BearerAuth
// @in                          header
// @name                        Authorization
// @description                 RS256 JWT — format: Bearer {token}

// @securityDefinitions.apikey  UserToken
// @in                          header
// @name                        X-User-Token
// @description                 UUID user session token

func main() {
	// Immediate output to ensure we see SOMETHING in Render logs
	fmt.Fprintf(os.Stderr, "QueueBuzz binary started at %s\n", time.Now().Format(time.RFC3339))

	log.Log.Info().Msg("Starting QueueBuzz process...")

	healthCheck := flag.Bool("health", false, "Run healthcheck and exit")
	flag.Parse()
	cfg := config.Get()
	log.Init(cfg.IsProduction())

	log.Info().
		Str("env", cfg.AppEnv).
		Str("port", cfg.AppPort).
		Msg("Configuration and logging initialized")

	if *healthCheck {
		log.Info().Msg("Executing health-check...")

		client := http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://localhost:" + cfg.AppPort + "/healthz")
		if err != nil {
			log.Error().Err(err).Msg("Healthcheck failed: server unreachable")
			os.Exit(1)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			log.Error().
				Int("status", resp.StatusCode).
				Msg("Healthcheck failed: unhealthy status code")
			os.Exit(1)
		}

		log.Info().Msg("Healthcheck: OK")
		os.Exit(0)
	}

	log.Info().Msg("Building application container...")

	_, db := mongo.Connect(cfg.DBUri, cfg.DBName)
	seed.Run(context.Background(), db)

	sender := firebase.NewSender(cfg.FirebaseCredentials)
	application := app.New(cfg, sender)

	log.Info().Msg("Starting Fiber server listener...")
	application.Start()
}
