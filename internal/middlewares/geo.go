package middlewares

import (
	"strconv"
	"sync"

	"queuebuzz/internal/services"

	"github.com/gofiber/fiber/v3"
)

var (
	geoService *services.GeoService
	geoOnce    sync.Once
)

// InitGeoMiddleware sets the geo service for the middleware.
func InitGeoMiddleware(svc *services.GeoService) {
	geoOnce.Do(func() {
		geoService = svc
	})
}

// GeoMiddleware validates that the user's coordinates are within the queue's geofence.
// Expects lat/lng as query parameters.
func GeoMiddleware() fiber.Handler {
	return func(c fiber.Ctx) error {
		latStr := c.Query("lat")
		lngStr := c.Query("lng")

		if latStr == "" || lngStr == "" {
			return fiber.NewError(fiber.StatusBadRequest, "lat and lng are required")
		}

		lat, err := strconv.ParseFloat(latStr, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid lat value")
		}

		lng, err := strconv.ParseFloat(lngStr, 64)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid lng value")
		}

		if err := geoService.ValidateCoords(lat, lng); err != nil {
			return err
		}

		c.Locals("user_lat", lat)
		c.Locals("user_lng", lng)

		return c.Next()
	}
}
