package e2e

import (
	"context"
	"fmt"
	"net/http"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSE_HostStream_ReceivesEvent(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, hostToken := util.CreateQueue(t, s, "SSE Host Queue")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := util.ReadSSE(ctx, s,
		fmt.Sprintf("/api/v1/queue/manage/%s/events", queueID),
		[]*http.Cookie{util.HostCookie(hostToken)},
	)
	require.NoError(t, err)

	// Clear initial snapshot events
	timer := time.NewTimer(1 * time.Second)
drain:
	for {
		select {
		case <-ch:
		case <-timer.C:
			break drain
		}
	}

	// Small delay to ensure server and client are stable
	time.Sleep(100 * time.Millisecond)

	util.JoinQueue(t, s, queueID, "SSE Customer")

	select {
	case ev := <-ch:
		// Host receives 'joined' event when customer joins
		assert.Contains(t, []string{"joined", "queue_update"}, ev.Event)
		assert.NotEmpty(t, ev.Data)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for SSE event after join")
	}
}

func TestSSE_PublicStream_ReceivesEvent(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, _ := util.CreateQueue(t, s, "SSE Public Queue")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := util.ReadSSE(ctx, s,
		fmt.Sprintf("/api/v1/queue/p/%s/events", queueID),
		nil,
	)
	require.NoError(t, err)

	// Clear initial snapshot
	timer := time.NewTimer(1 * time.Second)
drain:
	for {
		select {
		case <-ch:
		case <-timer.C:
			break drain
		}
	}

	time.Sleep(100 * time.Millisecond)
	util.JoinQueue(t, s, queueID, "Public SSE Customer")

	select {
	case ev := <-ch:
		assert.Equal(t, "waiting_count_updated", ev.Event)
		assert.NotEmpty(t, ev.Data)
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for public SSE event")
	}
}

func TestSSE_Unauthorized(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, _ := util.CreateQueue(t, s, "SSE Auth Queue")

	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/manage/%s/events", queueID))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSSE_BroadcastIsolation(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID1, hostToken1 := util.CreateQueue(t, s, "SSE Isolation 1")
	queueID2, _ := util.CreateQueue(t, s, "SSE Isolation 2")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ch, err := util.ReadSSE(ctx, s,
		fmt.Sprintf("/api/v1/queue/manage/%s/events", queueID1),
		[]*http.Cookie{util.HostCookie(hostToken1)},
	)
	require.NoError(t, err)

	// Clear initial snapshot
	timer := time.NewTimer(500 * time.Millisecond)
drain:
	for {
		select {
		case <-ch:
		case <-timer.C:
			break drain
		}
	}

	util.JoinQueue(t, s, queueID2, "Queue2 Customer")

	select {
	case ev := <-ch:
		t.Fatalf("unexpected cross-queue event: %+v", ev)
	case <-time.After(500 * time.Millisecond):
		// Success: isolation preserved
	}
}
