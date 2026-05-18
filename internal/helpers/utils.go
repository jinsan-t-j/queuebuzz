package helpers

import (
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"strings"
)

func DerefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

/**
 * GenerateSlug creates a random URL-friendly, unique-ish identifier.
 * Uses a random adjective-noun-hex triplet to minimize collisions.
 */
func GenerateSlug() string {
	adjectives := []string{"swift", "bright", "calm", "bold", "cool", "fast", "keen", "neat", "warm", "wise", "bold", "crisp", "pure", "vivid"}
	nouns := []string{"queue", "spot", "line", "desk", "gate", "lane", "zone", "hub", "dock", "pass", "spot", "flow", "list", "path"}

	adj := adjectives[randInt(len(adjectives))]
	noun := nouns[randInt(len(nouns))]

	// Append a 4-char random hex suffix for uniqueness (16^4 = 65,536 combinations per base)
	return fmt.Sprintf("%s-%s-%s", adj, noun, randomHex(4))
}

func randInt(limit int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	return int(n.Int64())
}

func randomHex(length int) string {
	const charset = "0123456789abcdef"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[randInt(len(charset))]
	}
	return string(b)
}
func MaskEmail(email *string) *string {
	if email == nil || *email == "" {
		return nil
	}
	parts := strings.Split(*email, "@")
	if len(parts) != 2 {
		masked := "***"
		return &masked
	}
	name := parts[0]
	domain := parts[1]
	if len(name) > 2 {
		name = name[:2] + "****"
	} else {
		name = "****"
	}
	masked := name + "@" + domain
	return &masked
}

func MaskPhone(phone *string) *string {
	if phone == nil || *phone == "" {
		return nil
	}
	p := *phone
	if len(p) > 4 {
		masked := "*******" + p[len(p)-3:]
		return &masked
	}
	masked := "*******"
	return &masked
}

func CalculateDistance(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadius = 6371000 // Earth radius in meters

	dLat := (lat2 - lat1) * math.Pi / 180.0
	dLon := (lon2 - lon1) * math.Pi / 180.0

	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180.0)*math.Cos(lat2*math.Pi/180.0)*
			math.Sin(dLon/2)*math.Sin(dLon/2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return earthRadius * c
}
