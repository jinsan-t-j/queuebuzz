package events

import (
	"queuebuzz/internal/sse"
)

// List of all SSE event types used in the customer module.
const (
	EventPositionUpdate     = "position_update"
	EventEntryStatusChanged = "entry_status_changed"
	EventEntryInit          = "entry_init"
	EventPushTokenRefresh   = "push_token_refresh_required"
)

// PositionUpdateData is the payload for EventPositionUpdate (entry-scoped topic).
type PositionUpdateData struct {
	Position int64 `json:"position"`
}

// EntryStatusChangedData is the payload for EventEntryStatusChanged.
type EntryStatusChangedData struct {
	Status string `json:"status"`
}

// PushTokenRefreshData tells the customer app to re-sync its browser FCM token.
type PushTokenRefreshData struct {
	Reason string `json:"reason"`
}

// Wrap is a helper to wrap payload in an SSE message.
func Wrap(event string, data interface{}) sse.Message {
	return sse.NewMessage(event, data)
}
