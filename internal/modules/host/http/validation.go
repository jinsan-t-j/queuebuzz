package http

import (
	"fmt"
	"mime/multipart"
	"strings"
)

// allowedImageMIMEs lists the MIME types accepted for host branding images.
var allowedImageMIMEs = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

func validateImageFile(fh *multipart.FileHeader, maxBytes int) error {
	if fh.Size > int64(maxBytes) {
		maxMB := float64(maxBytes) / (1024 * 1024)
		actualMB := float64(fh.Size) / (1024 * 1024)
		return fmt.Errorf("file too large (%.2fMB), maximum allowed: %.1fMB", actualMB, maxMB)
	}

	contentType := fh.Header.Get("Content-Type")
	if !allowedImageMIMEs[contentType] {
		return fmt.Errorf("unsupported image type %q, allowed: JPEG, PNG, WebP, GIF", contentType)
	}

	return nil
}

// GetExtensionFromMIME returns the file extension for a given MIME type.
func GetExtensionFromMIME(mime string) string {
	switch {
	case strings.Contains(mime, "jpeg"):
		return ".jpg"
	case strings.Contains(mime, "png"):
		return ".png"
	case strings.Contains(mime, "webp"):
		return ".webp"
	case strings.Contains(mime, "gif"):
		return ".gif"
	default:
		return ".png"
	}
}
