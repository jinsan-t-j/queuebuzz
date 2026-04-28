package http

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// allowedImageMIMEs lists the MIME types accepted for host branding images.
var allowedImageMIMEs = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

// validateImageDataURL validates a base64 data URL value for size and MIME type.
// An empty string is allowed (used to clear/remove the image).
func validateImageDataURL(val interface{}, maxBytes int) error {
	s, ok := val.(string)
	if !ok {
		return fmt.Errorf("must be a string")
	}

	// Allow clearing the image
	if s == "" {
		return nil
	}

	// Expect format: data:<mime>;base64,<payload>
	if !strings.HasPrefix(s, "data:") {
		return fmt.Errorf("must be a valid data URL")
	}

	// Extract MIME type
	parts := strings.SplitN(s, ";", 2)
	if len(parts) < 2 {
		return fmt.Errorf("invalid data URL format")
	}
	mime := strings.TrimPrefix(parts[0], "data:")

	if !allowedImageMIMEs[mime] {
		return fmt.Errorf("unsupported image type %q, allowed: JPEG, PNG, WebP, GIF", mime)
	}

	// Extract base64 payload
	b64Parts := strings.SplitN(parts[1], ",", 2)
	if len(b64Parts) < 2 {
		return fmt.Errorf("invalid data URL format")
	}
	payload := b64Parts[1]

	// Decode to check actual size
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		// Try RawStdEncoding (no padding) as browsers sometimes omit padding
		decoded, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return fmt.Errorf("invalid base64 data")
		}
	}

	if len(decoded) > maxBytes {
		maxMB := float64(maxBytes) / (1024 * 1024)
		return fmt.Errorf("image too large (%.1fMB), maximum allowed: %.1fMB", float64(len(decoded))/(1024*1024), maxMB)
	}

	return nil
}

// ParseImageDataURL parses a base64 data URL into raw bytes and content type.
func ParseImageDataURL(s string) ([]byte, string, error) {
	if !strings.HasPrefix(s, "data:") {
		return nil, "", fmt.Errorf("invalid data URL prefix")
	}

	parts := strings.SplitN(s, ";", 2)
	if len(parts) < 2 {
		return nil, "", fmt.Errorf("invalid data URL format")
	}
	mime := strings.TrimPrefix(parts[0], "data:")

	b64Parts := strings.SplitN(parts[1], ",", 2)
	if len(b64Parts) < 2 {
		return nil, "", fmt.Errorf("invalid data URL format")
	}
	payload := b64Parts[1]

	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decode base64")
		}
	}

	return decoded, mime, nil
}
