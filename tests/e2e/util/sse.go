package util

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"queuebuzz/tests/e2e/setup"
	"strings"
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
}

// ReadSSE streams events from an SSE endpoint.
func ReadSSE(ctx context.Context, s *setup.TestSuite, path string, cookies []*http.Cookie) (<-chan SSEEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Use a fresh client for SSE to avoid connection pooling/cookie jar interference
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("sse: status %d", resp.StatusCode)
	}

	ch := make(chan SSEEvent, 32)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		var ev SSEEvent
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			line := scanner.Text()
			if line == "" {
				if ev.Data != "" || ev.Event != "" {
					select {
					case ch <- ev:
					case <-ctx.Done():
						return
					}
					ev = SSEEvent{}
				}
				continue
			}
			if k, v, ok := strings.Cut(line, ":"); ok {
				switch k {
				case "event":
					ev.Event = strings.TrimSpace(v)
				case "data":
					ev.Data = strings.TrimSpace(v)
				case "id":
					ev.ID = strings.TrimSpace(v)
				}
			}
		}
	}()

	return ch, nil
}
