package events

import (
	"queuebuzz/internal/sse"
)

// List of all SSE event types used in the queue module.
const (
	EventUserJoined          = "user_joined"
	EventUserLeft            = "user_left"
	EventUserStatusChanged   = "user_status_changed"
	EventUserCalled          = "user_called"
	EventQueueStatusChanged  = "queue_status_changed"
	EventQueueUpdate         = "queue_update"
	EventQueueExpired        = "queue_expired"
	EventPositionUpdate      = "position_update"
	EventEntryStatusChanged  = "entry_status_changed"
	EventQueueInit           = "queue_init"
	EventWaitingCountUpdated = "waiting_count_updated"
)

// UserStatusData is the payload for EventUserStatusChanged.
type UserStatusData struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// QueueExpiredData is the payload for EventQueueExpired.
type QueueExpiredData struct {
	QueueID string `json:"queue_id"`
}

// QueueStatusData is the payload for EventQueueStatusChanged.
type QueueStatusData struct {
	Status string `json:"status"`
}

// PositionUpdateData is the payload for EventPositionUpdate (entry-scoped topic).
type PositionUpdateData struct {
	Position int64 `json:"position"`
}

// EntryStatusChangedData is the payload for EventEntryStatusChanged.
type EntryStatusChangedData struct {
	Status string `json:"status"`
}

// Wrap is a helper to wrap payload in an SSE message.
func Wrap(event string, data interface{}) sse.Message {
	return sse.NewMessage(event, data)
}
