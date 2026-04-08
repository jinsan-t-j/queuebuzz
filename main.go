package main

import (
	"flag"
	"net/http"
	"os"
	"queuebuzz/internal/app"
	"queuebuzz/internal/config"

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
	healthCheck := flag.Bool("health", false, "Run healthcheck and exit")
	flag.Parse()

	if *healthCheck {
		cfg := config.Get()
		resp, err := http.Get("http://localhost:" + cfg.AppPort + "/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	application := app.New()

	application.Start()
}
