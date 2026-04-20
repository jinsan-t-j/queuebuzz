package helpers

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// ToPascalCase converts a snake_case string to PascalCase.
func ToPascalCase(s string) string {
	words := strings.Split(s, "_")
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
}

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
