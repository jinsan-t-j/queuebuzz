package e2e

import (
	"fmt"
	"net/http"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHost_UpdateSettings_WithImages(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Register a host
	email := fmt.Sprintf("settings-%d@example.com", time.Now().UnixNano())
	hostToken := util.RegisterHost(t, s, "Original Name", email, "password123")

	// 2. Prepare tiny 1x1 base64 images
	// Profile: blue dot, Banner: red dot
	profileImage := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAEfQC5f96T8QAAAABJRU5ErkJggg=="
	bannerImage := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

	// 3. Update settings
	updates := map[string]any{
		"name":              "Updated Business Name",
		"profile_image_url": profileImage,
		"banner_image_url":  bannerImage,
	}

	resp, err := util.PATCH(s, "/api/v1/host/me", updates, util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 4. Verify updates
	resp, err = util.GET(s, "/api/v1/host/me", util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	assert.Equal(t, "Updated Business Name", data["name"])

	// URLs should point to our Minio instance
	profileURL := data["profile_image_url"].(string)
	bannerURL := data["banner_image_url"].(string)

	assert.Contains(t, profileURL, "/queuebuzz-test/hosts/")
	assert.Contains(t, profileURL, "profile.png")
	assert.Contains(t, bannerURL, "/queuebuzz-test/hosts/")
	assert.Contains(t, bannerURL, "banner.png")

	// 5. Verify images are actually accessible (Minio is public in our test setup)
	resp, err = http.Get(profileURL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "image/png", resp.Header.Get("Content-Type"))
}

func TestHost_UpdateSettings_ClearImages(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	email := fmt.Sprintf("clear-%d@example.com", time.Now().UnixNano())
	hostToken := util.RegisterHost(t, s, "Image Host", email, "password123")

	// Set an image first
	profileImage := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAEfQC5f96T8QAAAABJRU5ErkJggg=="
	_, _ = util.PATCH(s, "/api/v1/host/me", map[string]any{"profile_image_url": profileImage}, util.AuthCookie(hostToken))

	// Now clear it
	resp, err := util.PATCH(s, "/api/v1/host/me", map[string]any{"profile_image_url": ""}, util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify it's null
	resp, err = util.GET(s, "/api/v1/host/me", util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	data := util.DecodedBody(t, resp)
	assert.Nil(t, data["profile_image_url"])
}
