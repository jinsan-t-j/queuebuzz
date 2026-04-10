package middlewares

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
)

func CORSMiddleware(allowedOrigin string) fiber.Handler {
	origins := strings.Split(allowedOrigin, ",")
	for i, origin := range origins {
		origins[i] = strings.TrimSpace(origin)
	}

	return cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-User-Token"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowCredentials: true,
		MaxAge:           3600,
	})
}
