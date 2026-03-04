package ws

// Event type constants for WebSocket messages.
const (
	EventUserJoined        = "user_joined"
	EventUserLeft          = "user_left"
	EventUserStatusChanged = "user_status_changed"
	EventUserCalled        = "user_called"
	EventQueueUpdate       = "queue_update"
	EventQueueExpired      = "queue_expired"
	EventVisibility        = "visibility"
)

// Message is the standard WebSocket envelope sent to/from clients.
type Message struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// UserLeftData is the payload for EventUserLeft.
type UserLeftData struct {
	Token string `json:"token"`
}

// UserStatusData is the payload for EventUserStatusChanged.
type UserStatusData struct {
	Token  string `json:"token"`
	Status string `json:"status"`
}

// QueueExpiredData is the payload for EventQueueExpired.
type QueueExpiredData struct {
	QueueID string `json:"queue_id"`
}

// VisibilityData is the payload received from clients for EventVisibility.
type VisibilityData struct {
	Status string `json:"status"` // "background" | "foreground"
}
