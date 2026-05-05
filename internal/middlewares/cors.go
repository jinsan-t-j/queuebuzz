package middlewares

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
)

func CORSMiddleware(allowedOrigin string) fiber.Handler {
	origins := []string{}
	for _, origin := range strings.Split(allowedOrigin, ",") {
		trimmed := strings.TrimSpace(origin)
		if trimmed != "" {
			origins = append(origins, trimmed)
		}
	}

	if len(origins) == 0 {
		origins = []string{"*"}
	}

	return cors.New(cors.Config{
		AllowOrigins: origins,
		AllowHeaders: []string{
			"Origin",
			"Content-Type",
			"Accept",
			"Authorization",
			"X-User-Token",
			"Cache-Control",
			"Connection",
			"X-Browser-Language",
			"X-Browser-Timezone",
			"CF-IPCountry",
			"X-Vercel-IP-Country",
			"x-browser-language",
			"x-browser-timezone",
		},
		AllowMethods:        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowCredentials:    true,
		AllowPrivateNetwork: true,
		MaxAge:              3600,
	})
}
