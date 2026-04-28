package e2e

import (
	"fmt"
	"net/http"
	"queuebuzz/tests/e2e/util"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResilience_ReadYourWrites_Consistent(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, hostToken := util.CreateQueue(t, s, "Consistency Queue")

	// Write: Join
	util.JoinQueue(t, s, queueID, "Resilient User")

	// Read: Check history (must be immediately visible)
	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/manage/%s/history", queueID), util.HostCookie(hostToken))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	data := util.DecodedBody(t, resp)
	entries := data["entries"].([]any)

	assert.GreaterOrEqual(t, len(entries), 1, "Write must be immediately readable in history")
}
