package system

import (
	"fmt"
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/billing"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResilience_ReadYourWrites_Consistent(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// Register host (full lifecycle) and upgrade to pro for history access
	token, hostID, _ := authutil.RegisterHost(t, s, "Consistency Host", "consistency@test.com", "password")
	queueID, joinCode := qutil.CreateAuthenticatedQueue(t, s, "Consistency Queue", token)
	billing.UpgradeToPremium(t, s, hostID, token)

	// Write: Join
	qutil.JoinQueue(t, s, queueID, joinCode, "Resilient User")

	// Read: Check history (must be immediately visible)
	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/manage/%s/history", queueID), util.AuthCookie(token))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	entries := data["entries"].([]any)

	assert.GreaterOrEqual(t, len(entries), 1, "Write must be immediately readable in history")
}
