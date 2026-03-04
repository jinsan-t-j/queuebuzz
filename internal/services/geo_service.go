package services

import (
	"math"
	"sync"

	"github.com/gofiber/fiber/v3"
)

var (
	geoInstance *GeoService
	geoOnce     sync.Once
)

type GeoService struct{}

func NewGeoService() *GeoService {
	geoOnce.Do(func() {
		geoInstance = &GeoService{}
	})

	return geoInstance
}

const earthRadiusM = 6_371_000.0 // Earth radius in meters

// HaversineDistance returns the great-circle distance in meters between two
// lat/lng points on the Earth's surface.
func (s *GeoService) HaversineDistance(lat1, lng1, lat2, lng2 float64) float64 {
	dLat := degreesToRadians(lat2 - lat1)
	dLng := degreesToRadians(lng2 - lng1)

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(degreesToRadians(lat1))*math.Cos(degreesToRadians(lat2))*
			math.Sin(dLng/2)*math.Sin(dLng/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadiusM * c
}

// ValidateCoords checks that lat/lng are within valid ranges.
func (s *GeoService) ValidateCoords(lat, lng float64) error {
	if lat < -90 || lat > 90 {
		return fiber.NewError(fiber.StatusBadRequest, "latitude must be between -90 and 90")
	}
	if lng < -180 || lng > 180 {
		return fiber.NewError(fiber.StatusBadRequest, "longitude must be between -180 and 180")
	}
	return nil
}

// IsWithinRadius returns true if the user position is within radiusM of the host position.
func (s *GeoService) IsWithinRadius(hostLat, hostLng, userLat, userLng float64, radiusM int) bool {
	distance := s.HaversineDistance(hostLat, hostLng, userLat, userLng)
	return distance <= float64(radiusM)
}

func degreesToRadians(deg float64) float64 {
	return deg * math.Pi / 180
}
