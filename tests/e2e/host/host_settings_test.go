package host

import (
	"fmt"
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/billing"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHost_UpdateSettings_ClearImages(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	email := fmt.Sprintf("clear-%d@example.com", time.Now().UnixNano())
	hostToken, hostID, _ := authutil.RegisterHost(t, s, "Image Host", email, "password123")
	billing.UpgradeToElite(t, s, hostID)

	// Set an image first using Multipart
	profileData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41, 0x54, 0x08, 0xD7, 0x63, 0xF8, 0xFF, 0xFF, 0x3F, 0x00, 0x05, 0xFE, 0x02, 0xFE, 0xDC, 0x44, 0x74, 0x06, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	_, _ = util.PATCHForm(s, "/api/v1/host/me", nil, map[string][]byte{"profile_image": profileData}, util.AuthCookie(hostToken))

	// Now clear it using JSON PATCH with an empty string
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

func TestHost_UpdateSettings_WithImages(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Register a host
	email := fmt.Sprintf("multipart-%d@example.com", time.Now().UnixNano())
	hostToken, hostID, _ := authutil.RegisterHost(t, s, "Multipart Host", email, "password123")
	billing.UpgradeToElite(t, s, hostID)

	// 2. Prepare raw binary images (simulating frontend Blobs)
	// Just 1x1 pixels
	profileData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41, 0x54, 0x08, 0xD7, 0x63, 0xF8, 0xFF, 0xFF, 0x3F, 0x00, 0x05, 0xFE, 0x02, 0xFE, 0xDC, 0x44, 0x74, 0x06, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}
	bannerData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41, 0x54, 0x08, 0xD7, 0x63, 0xF8, 0xFF, 0xFF, 0x3F, 0x00, 0x05, 0xFE, 0x02, 0xFE, 0xDC, 0x44, 0x74, 0x06, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82}

	fields := map[string]string{
		"name": "Multipart Business",
	}
	files := map[string][]byte{
		"profile_image": profileData,
		"banner_image":  bannerData,
	}

	// 3. Update via Multipart
	resp, err := util.PATCHForm(s, "/api/v1/host/me", fields, files, util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 4. Verify
	resp, err = util.GET(s, "/api/v1/host/me", util.AuthCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()

	data := util.DecodedBody(t, resp)
	assert.Equal(t, "Multipart Business", data["name"])
	assert.Contains(t, data["profile_image_url"], "profile.png")
	assert.Contains(t, data["banner_image_url"], "banner.png")
}

func TestHost_BrandingGuard(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	email := fmt.Sprintf("branding-%d@test.com", time.Now().UnixNano())
	token, hostID, _ := authutil.RegisterHost(t, s, "Branding Host", email, "password123")

	t.Run("Free plan blocked from updating images", func(t *testing.T) {
		fields := map[string]string{"name": "Blocked Business"}
		files := map[string][]byte{"profile_image": {0x89, 0x50, 0x4E, 0x47, 0x0D}}
		resp, err := util.PATCHForm(s, "/api/v1/host/me", fields, files, util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	billing.UpgradeToElite(t, s, hostID)

	t.Run("Elite plan allowed to update images", func(t *testing.T) {
		fields := map[string]string{"name": "Elite Business"}
		files := map[string][]byte{"profile_image": {0x89, 0x50, 0x4E, 0x47, 0x0D}}
		resp, err := util.PATCHForm(s, "/api/v1/host/me", fields, files, util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
