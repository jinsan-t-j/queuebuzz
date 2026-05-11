package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsValidSlug(t *testing.T) {
	tests := []struct {
		slug     string
		expected bool
	}{
		{"valid-slug", true},
		{"valid123", true},
		{"123valid", true},
		{"a-b-c", true},
		{"ab", false},                               // too short
		{"a", false},                                // too short
		{"this-slug-is-exactly-30-char-z", true},    // 30 chars
		{"this-slug-is-exactly-31-chars-zz", false}, // too long
		{"-starts-with-hyphen", false},
		{"ends-with-hyphen-", false},
		{"invalid_underscore", false},
		{"invalid!char", false},
		{"invalid space", false},
		{"Uppercase", false}, // Regex only allows lowercase
	}

	for _, tt := range tests {
		t.Run(tt.slug, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsValidSlug(tt.slug))
		})
	}
}
