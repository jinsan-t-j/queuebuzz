package queue

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestQueue_CustomSlugRules(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	anonResp, err := util.POST(s, "/api/v1/queue/p/create", map[string]any{
		"name": "Anonymous Slug Queue",
		"slug": "custom-anon-slug",
	})
	require.NoError(t, err)
	defer anonResp.Body.Close()
	assert.Equal(t, http.StatusForbidden, anonResp.StatusCode)

	token, _, _ := authutil.RegisterHost(t, s, "Slug Host", "slug@test.com", "password")

	invalidResp, err := util.POST(s, "/api/v1/queue/p/create", map[string]any{
		"name": "Invalid Slug Queue",
		"slug": "Bad Slug!",
	}, util.AuthCookie(token))
	require.NoError(t, err)
	defer invalidResp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, invalidResp.StatusCode)

	validResp, err := util.POST(s, "/api/v1/queue/p/create", map[string]any{
		"name": "Custom Slug Queue",
		"slug": "custom-slug-queue",
	}, util.AuthCookie(token))
	require.NoError(t, err)
	defer validResp.Body.Close()
	assert.Equal(t, http.StatusCreated, validResp.StatusCode)

	data := util.DecodedBody(t, validResp)
	assert.Equal(t, "custom-slug-queue", data["slug"])

	var stored struct {
		Slug string `bson:"slug"`
	}
	require.NoError(t, s.DB.Collection("queues").FindOne(context.Background(), bson.M{"_id": data["id"]}).Decode(&stored))
	assert.Equal(t, "custom-slug-queue", stored.Slug)
}

func TestQueue_SlugCheck_EdgeCases(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	token, _, _ := authutil.RegisterHost(t, s, "Slug Check Host", "slug-check@test.com", "password")

	reservedResp, err := util.GET(s, "/api/v1/queue/slug-check?slug=dashboard", util.AuthCookie(token))
	require.NoError(t, err)
	defer reservedResp.Body.Close()
	assert.Equal(t, http.StatusOK, reservedResp.StatusCode)

	reservedData := util.DecodedBody(t, reservedResp)
	assert.Equal(t, false, reservedData["is_available"])

	invalidResp, err := util.GET(s, "/api/v1/queue/slug-check?slug=Bad%20Slug", util.AuthCookie(token))
	require.NoError(t, err)
	defer invalidResp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, invalidResp.StatusCode)

	// Valid available slug
	availResp, err := util.GET(s, "/api/v1/queue/slug-check?slug=my-custom-queue", util.AuthCookie(token))
	require.NoError(t, err)
	defer availResp.Body.Close()
	assert.Equal(t, http.StatusOK, availResp.StatusCode)
	availData := util.DecodedBody(t, availResp)
	assert.Equal(t, true, availData["is_available"])

	// Taken slug
	_, err = util.POST(s, "/api/v1/queue/p/create", map[string]any{
		"name": "Existing Queue",
		"slug": "taken-slug",
	}, util.AuthCookie(token))
	require.NoError(t, err)

	takenResp, err := util.GET(s, "/api/v1/queue/slug-check?slug=taken-slug", util.AuthCookie(token))
	require.NoError(t, err)
	defer takenResp.Body.Close()
	assert.Equal(t, http.StatusOK, takenResp.StatusCode)
	takenData := util.DecodedBody(t, takenResp)
	assert.Equal(t, false, takenData["is_available"])

	// Reserved slugs should return is_available: false, not 400
	reservedSlugs := []string{"admin", "api", "auth", "settings"}
	for _, rs := range reservedSlugs {
		resp, err := util.GET(s, "/api/v1/queue/slug-check?slug="+rs, util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		data := util.DecodedBody(t, resp)
		assert.Equal(t, false, data["is_available"], "Slug %s should be reserved", rs)
	}

	// Invalid formats (too short, too long, invalid chars)
	invalidSlugs := []string{"ab", "this-slug-is-way-too-long-more-than-30-chars", "slug!", "slug_underscore", "-start-with-hyphen", "end-with-hyphen-"}
	for _, is := range invalidSlugs {
		resp, err := util.GET(s, "/api/v1/queue/slug-check?slug="+is, util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, "Slug %s should be invalid format", is)
	}
}

func TestQueue_AnonymousExpiry_HardDeletes(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	ctx := context.Background()
	createResp, err := util.POST(s, "/api/v1/queue/p/create", map[string]any{
		"name": "Anonymous Expiry Queue",
	})
	require.NoError(t, err)
	defer createResp.Body.Close()
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	createData := util.DecodedBody(t, createResp)
	queueID := createData["id"].(string)
	joinCode := createData["join_code"].(string)

	joinResp, err := util.POST(s, fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID), map[string]any{
		"display_name": "Guest One",
		"email":        "guest-one@example.com",
		"fingerprint":  "fp-anon-expiry-1",
		"join_code":    joinCode,
	})
	require.NoError(t, err)
	defer joinResp.Body.Close()
	require.Equal(t, http.StatusCreated, joinResp.StatusCode)

	_, err = s.DB.Collection("queues").UpdateOne(ctx,
		bson.M{"_id": queueID},
		bson.M{"$set": bson.M{"expires_at": time.Now().Add(-1 * time.Hour)}},
	)
	require.NoError(t, err)

	require.NoError(t, s.App.Container.Queue.ExpiryJob.Run(ctx))

	var queueDoc bson.M
	err = s.DB.Collection("queues").FindOne(ctx, bson.M{"_id": queueID}).Decode(&queueDoc)
	assert.Error(t, err)

	remainingEntries, err := s.DB.Collection("queue_entries").CountDocuments(ctx, bson.M{"queue_id": queueID})
	require.NoError(t, err)
	assert.Equal(t, int64(0), remainingEntries)
}
