package queue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/billing"
	"queuebuzz/tests/e2e/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateQueue_GeoLock_FreeHost_Forbidden(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Register a host (starts on Free plan by default)
	token, _, _ := authutil.RegisterHost(t, s, "Free Host", "freehost@test.com", "password")

	// 2. Attempt to create a geo-locked queue
	payload := map[string]any{
		"name":              "Geo Queue",
		"is_geo_locked":     true,
		"latitude":          12.9716,
		"longitude":         77.5946,
		"geo_radius_meters": 100,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/queue/p/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(util.AuthCookie(token))

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should be Forbidden because host is on free tier
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestCreateQueue_GeoLock_AnonymousHost_Forbidden(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// Attempt to create a geo-locked queue anonymously (without host token)
	payload := map[string]any{
		"name":              "Geo Queue",
		"is_geo_locked":     true,
		"latitude":          12.9716,
		"longitude":         77.5946,
		"geo_radius_meters": 100,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/queue/p/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should be Forbidden because anonymous queue creation does not support premium features
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestJoinQueue_GeoLock_Validation(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Register a premium host
	token, hostID, _ := authutil.RegisterHost(t, s, "Premium Host", "premiumhost@test.com", "password")
	billing.UpgradeToPremium(t, s, hostID, token)

	// Bangalore coordinates (lat: 12.9716, lng: 77.5946)
	hostLat := 12.9716
	hostLng := 77.5946
	radius := 500.0 // 500 meters

	// 2. Create geo-locked queue
	payload := map[string]any{
		"name":              "Geo-Locked Clinic",
		"is_geo_locked":     true,
		"latitude":          hostLat,
		"longitude":         hostLng,
		"geo_radius_meters": radius,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/queue/p/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(util.AuthCookie(token))

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	queueID := data["id"].(string)
	joinCode := data["join_code"].(string)

	// 3. Attempt to join without coordinates (should fail with 400 Bad Request)
	joinPayload := map[string]any{
		"display_name": "Guest Missing Location",
		"fingerprint":  "missing-loc-fingerprint",
		"join_code":    joinCode,
	}
	joinBody, _ := json.Marshal(joinPayload)
	req, _ = http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(joinBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// 4. Attempt to join with coordinates far away (e.g. Mumbai ~840km away, should fail with 403 Forbidden)
	joinPayload = map[string]any{
		"display_name": "Guest Far Away",
		"fingerprint":  "far-away-fingerprint",
		"join_code":    joinCode,
		"latitude":     19.0760,
		"longitude":    72.8777,
	}
	joinBody, _ = json.Marshal(joinPayload)
	req, _ = http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(joinBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// 5. Attempt to join with coordinates close by (~100m away, should succeed with 201 Created)
	// 0.0009 degrees lat is roughly 100 meters
	joinPayload = map[string]any{
		"display_name": "Guest Close By",
		"fingerprint":  "close-by-fingerprint",
		"join_code":    joinCode,
		"latitude":     hostLat + 0.0005,
		"longitude":    hostLng,
	}
	joinBody, _ = json.Marshal(joinPayload)
	req, _ = http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(joinBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}

func TestUpdateQueue_GeoLock_Validation(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Create a queue as regular user first (free tier)
	token, hostID, _ := authutil.RegisterHost(t, s, "Tier Host", "tierhost@test.com", "password")
	body, _ := json.Marshal(map[string]any{"name": "Regular Queue"})
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/queue/p/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(util.AuthCookie(token))

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	queueID := data["id"].(string)

	// 2. Attempt to update Geo-Lock properties as a FREE host (should fail with 403 Forbidden)
	updatePayload := map[string]any{
		"is_geo_locked":     true,
		"latitude":          12.9716,
		"longitude":         77.5946,
		"geo_radius_meters": 150,
	}
	updateBody, _ := json.Marshal(updatePayload)
	req, _ = http.NewRequest(http.MethodPatch, s.BaseURL+fmt.Sprintf("/api/v1/queue/manage/%s", queueID), bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(util.AuthCookie(token))

	resp, err = s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)

	// 3. Upgrade to premium plan
	billing.UpgradeToPremium(t, s, hostID, token)

	// 4. Update Geo-Lock properties as PREMIUM host (should succeed with 200 OK)
	req2, _ := http.NewRequest(http.MethodPatch, s.BaseURL+fmt.Sprintf("/api/v1/queue/manage/%s", queueID), bytes.NewReader(updateBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(util.AuthCookie(token))

	resp, err = s.Do(req2)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestJoinQueue_NoGeoLock_Succeeds(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Register a host
	token, _, _ := authutil.RegisterHost(t, s, "Normal Host", "normalhost@test.com", "password")

	// 2. Create standard queue (no geo-lock)
	payload := map[string]any{
		"name":          "Normal Clinic",
		"is_geo_locked": false,
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, s.BaseURL+"/api/v1/queue/p/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(util.AuthCookie(token))

	resp, err := s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	queueID := data["id"].(string)
	joinCode := data["join_code"].(string)

	// 3. Attempt to join without coordinates (should succeed with 201 Created)
	joinPayload := map[string]any{
		"display_name": "Guest Without Location",
		"fingerprint":  "normal-guest-fingerprint",
		"join_code":    joinCode,
	}
	joinBody, _ := json.Marshal(joinPayload)
	req, _ = http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), bytes.NewReader(joinBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err = s.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
}
