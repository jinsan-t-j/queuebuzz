package main

import (
	"queuebuzz/internal/app"
	"queuebuzz/internal/log"
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
	log.Init()
	log.Info().Msg("Starting QueueBuzz server")

	application := app.New()

	log.Info().Msg("QueueBuzz server ready")
	application.Start()
}
