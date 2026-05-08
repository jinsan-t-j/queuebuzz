package system

import (
	"context"
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestSystem_SettingsSeedingAndImpact(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	t.Run("Settings are seeded on startup", func(t *testing.T) {
		var settings map[string]any
		err := s.DB.Collection("system_settings").FindOne(context.Background(), bson.M{"_id": "global"}).Decode(&settings)
		require.NoError(t, err)
		assert.Equal(t, "support@test.com", settings["support_email"])
		assert.Equal(t, int32(6), settings["default_queue_join_code_length"])
	})

	t.Run("Join code length from settings is respected", func(t *testing.T) {
		// TEST_SETUP: Modify system setting to 8-digit codes (no admin API exists)
		_, err := s.DB.Collection("system_settings").UpdateOne(
			context.Background(),
			bson.M{"_id": "global"},
			bson.M{"$set": bson.M{"default_queue_join_code_length": 8}},
		)
		require.NoError(t, err)

		// User flow: register host → create queue → verify join code length
		token, _, _ := authutil.RegisterHost(t, s, "Settings Host", "settings@test.com", "password")
		payload := map[string]any{"name": "Long Code Queue"}
		resp, err := util.POST(s, "/api/v1/queue/p/create", payload, util.AuthCookie(token))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var body map[string]any
		require.NoError(t, util.DecodeJSON(resp, &body))
		data := body["data"].(map[string]any)
		joinCode := data["join_code"].(string)

		assert.Equal(t, 8, len(joinCode), "join code length should match system settings")
	})
}
