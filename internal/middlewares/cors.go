package middlewares

import (
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
)

// CORSMiddleware creates a strict CORS middleware using the allowed origin.
func CORSMiddleware(allowedOrigin string) fiber.Handler {
	return cors.New(cors.Config{
		AllowOrigins:     []string{allowedOrigin},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "X-User-Token"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowCredentials: true,
	})
}
