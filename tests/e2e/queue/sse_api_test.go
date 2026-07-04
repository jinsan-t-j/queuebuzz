package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	authutil "queuebuzz/tests/e2e/auth/utils"
	"queuebuzz/tests/e2e/billing"
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
	found := false
	timeout := time.After(2 * time.Second)
	for !found {
		select {
		case ev := <-ch1:
			if ev.Event == "waiting_count_updated" {
				found = true
			}
		case <-timeout:
			t.Fatal("timed out waiting for event on Q1")
		}
	}

	select {
	case ev := <-ch2:
		t.Fatalf("unexpected event on Q2: %+v", ev)
	case <-time.After(500 * time.Millisecond):
		// Success: Q2 is isolated
	}
}

func TestSSE_PublicStream_Slug_ReceivesEvent(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	queueID, joinCode, _ := qutil.CreateQueue(t, s, "SSE Public Slug Queue")

	// Get slug using public status
	resp, err := util.GET(s, fmt.Sprintf("/api/v1/queue/p/%s/public-status", queueID))
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var data map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&data))
	qData := data["data"].(map[string]any)
	queueMap := qData["queue"].(map[string]any)
	slug := queueMap["slug"].(string)
	require.NotEmpty(t, slug)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Connect to public SSE stream using slug instead of ID
	ch, err := util.ReadSSE(ctx, s,
		fmt.Sprintf("/api/v1/queue/p/%s/events", slug),
		nil,
	)
	require.NoError(t, err)

	qutil.JoinQueue(t, s, queueID, joinCode, "Public Slug Customer")

	// Wait for the waiting_count_updated event to verify slug resolves, subscribes and gets events
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
			t.Fatal("timed out waiting for public slug SSE event")
		}
	}
}

func TestSSE_GuestDataMasking_FreeVsPremium(t *testing.T) {
	s := Suite(t)
	s.CleanDB()

	// 1. Register host (Free plan by default)
	hostToken, hostID, _ := authutil.RegisterHost(t, s, "Mask Host", "maskhost@test.com", "password")

	// 2. Create queue for free host
	queueID1, joinCode1 := qutil.CreateAuthenticatedQueue(t, s, "Free Host Queue", hostToken)

	// 3. Connect to SSE as free host
	ctx1, cancel1 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel1()
	ch1, err := util.ReadSSE(ctx1, s,
		fmt.Sprintf("/api/v1/queue/manage/%s/events", queueID1),
		[]*http.Cookie{util.AuthCookie(hostToken)},
	)
	require.NoError(t, err)

	// 4. Join customer with email and phone
	joinPayload1 := map[string]any{
		"display_name": "Customer One",
		"fingerprint":  "fp-cust-1",
		"join_code":    joinCode1,
		"email":        "customer1@example.com",
		"phone":        "9876543210",
	}
	bodyBytes1, _ := json.Marshal(joinPayload1)
	req1, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID1), bytes.NewReader(bodyBytes1))
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := s.Do(req1)
	require.NoError(t, err)
	resp1.Body.Close()
	require.Equal(t, http.StatusCreated, resp1.StatusCode)

	// 5. Verify email and phone are masked in host SSE event
	foundMasked := false
	timeout1 := time.After(5 * time.Second)
	for !foundMasked {
		select {
		case ev := <-ch1:
			if ev.Event == "joined" {
				var envelope map[string]any
				require.NoError(t, json.Unmarshal([]byte(ev.Data), &envelope))

				entryData, okEntry := envelope["data"].(map[string]any)
				require.True(t, okEntry)

				// Assert email and phone are masked
				email, okEmail := entryData["email"].(string)
				phone, okPhone := entryData["phone"].(string)
				assert.True(t, okEmail)
				assert.True(t, okPhone)

				assert.Contains(t, email, "****")
				assert.Contains(t, phone, "****")
				assert.NotEqual(t, "customer1@example.com", email)
				assert.NotEqual(t, "9876543210", phone)
				foundMasked = true
			}
		case <-timeout1:
			t.Fatal("timed out waiting for masked joined event")
		}
	}
	cancel1() // close stream 1

	// 5.5 Terminate first queue so host can start a new active queue
	qutil.TerminateQueue(t, s, queueID1, hostToken)

	// 6. Upgrade host to premium
	billing.UpgradeToPremium(t, s, hostID, hostToken)

	// 7. Create queue for premium host
	queueID2, joinCode2 := qutil.CreateAuthenticatedQueue(t, s, "Premium Host Queue", hostToken)

	// 8. Connect to SSE as premium host
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	ch2, err := util.ReadSSE(ctx2, s,
		fmt.Sprintf("/api/v1/queue/manage/%s/events", queueID2),
		[]*http.Cookie{util.AuthCookie(hostToken)},
	)
	require.NoError(t, err)

	// 9. Join customer to premium queue
	joinPayload2 := map[string]any{
		"display_name": "Customer Two",
		"fingerprint":  "fp-cust-2",
		"join_code":    joinCode2,
		"email":        "customer2@example.com",
		"phone":        "9876543211",
	}
	bodyBytes2, _ := json.Marshal(joinPayload2)
	req2, _ := http.NewRequest(http.MethodPost, s.BaseURL+fmt.Sprintf("/api/v1/customer/entry/join/%s", queueID2), bytes.NewReader(bodyBytes2))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := s.Do(req2)
	require.NoError(t, err)
	resp2.Body.Close()
	require.Equal(t, http.StatusCreated, resp2.StatusCode)

	// 10. Verify email and phone are UNMASKED in host SSE event
	foundUnmasked := false
	timeout2 := time.After(5 * time.Second)
	for !foundUnmasked {
		select {
		case ev := <-ch2:
			if ev.Event == "joined" {
				var envelope map[string]any
				require.NoError(t, json.Unmarshal([]byte(ev.Data), &envelope))

				entryData, okEntry := envelope["data"].(map[string]any)
				require.True(t, okEntry)

				// Assert email and phone are unmasked
				email, okEmail := entryData["email"].(string)
				phone, okPhone := entryData["phone"].(string)
				assert.True(t, okEmail)
				assert.True(t, okPhone)

				assert.Equal(t, "customer2@example.com", email)
				assert.Equal(t, "9876543211", phone)
				foundUnmasked = true
			}
		case <-timeout2:
			t.Fatal("timed out waiting for unmasked joined event")
		}
	}
}
