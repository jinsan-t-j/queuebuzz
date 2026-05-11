package queue

import (
	"context"
	"fmt"
	"net/http"
	qutil "queuebuzz/tests/e2e/queue/utils"
	"queuebuzz/tests/e2e/util"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSE_HostStream_ReceivesEvent(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, joinCode, hostToken := qutil.CreateQueue(t, s, "SSE Host Queue")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := util.ReadSSE(ctx, s,
		fmt.Sprintf("/api/v1/queue/manage/%s/events", queueID),
		[]*http.Cookie{util.HostCookie(hostToken)},
	)
	require.NoError(t, err)

	qutil.JoinQueue(t, s, queueID, joinCode, "SSE Customer")

	// Wait for the joined or queue_update event, skipping initial snapshot events
	found := false
	timeout := time.After(30 * time.Second)
	for !found {
		select {
		case ev := <-ch:
			if ev.Event == "joined" || ev.Event == "queue_update" {
				assert.NotEmpty(t, ev.Data)
				found = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for SSE event after join")
		}
	}
}

func TestSSE_PublicStream_ReceivesEvent(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "SSE Public Queue")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := util.ReadSSE(ctx, s,
		fmt.Sprintf("/api/v1/queue/p/%s/events", queueID),
		nil,
	)
	require.NoError(t, err)

	qutil.JoinQueue(t, s, queueID, joinCode, "Public SSE Customer")

	// Wait for the waiting_count_updated event, skipping initial snapshot
	found := false
	timeout := time.After(30 * time.Second)
	for !found {
		select {
		case ev := <-ch:
			if ev.Event == "waiting_count_updated" {
				assert.NotEmpty(t, ev.Data)
				found = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for public SSE event")
		}
	}
}

func TestSSE_Unauthorized(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, _, _ := qutil.CreateQueue(t, s, "SSE Auth Queue")

	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/manage/%s/events", queueID))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSSE_BroadcastIsolation(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID1, _, hostToken1 := qutil.CreateQueue(t, s, "SSE Isolation 1")
	queueID2, joinCode2, _ := qutil.CreateQueue(t, s, "SSE Isolation 2")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	qutil.JoinQueue(t, s, queueID2, joinCode2, "Queue2 Customer")

	select {
	case ev := <-ch:
		t.Fatalf("unexpected cross-queue event: %+v", ev)
	case <-time.After(500 * time.Millisecond):
		// Success: isolation preserved
	}
}

func TestSSE_Concurrent_IsolatedTopics(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Create two queues
	q1, jc1, _ := qutil.CreateQueue(t, s, "Queue 1")
	q2, _, _ := qutil.CreateQueue(t, s, "Queue 2")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 2. Subscribe to both public streams concurrently
	ch1, err := util.ReadSSE(ctx, s, fmt.Sprintf("/api/v1/queue/p/%s/events", q1), nil)
	require.NoError(t, err)

	ch2, err := util.ReadSSE(ctx, s, fmt.Sprintf("/api/v1/queue/p/%s/events", q2), nil)
	require.NoError(t, err)

	// Consume initial state events sent on connection
	<-ch1
	<-ch2

	// 3. Join Q1 only
	qutil.JoinQueue(t, s, q1, jc1, "Customer for Q1")

	// 4. Verify Q1 gets event, Q2 doesn't (isolation)
	select {
	case ev := <-ch1:
		assert.Equal(t, "waiting_count_updated", ev.Event)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event on Q1")
	}

	select {
	case ev := <-ch2:
		t.Fatalf("unexpected event on Q2: %+v", ev)
	case <-time.After(500 * time.Millisecond):
		// Success: Q2 is isolated
	}
}
