package sse

// Message is the standard SSE event envelope. The Event field is used by
// the broker to set the SSE "event:" line.
type Message struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

// NewMessage creates a new SSE message.
func NewMessage(event string, data interface{}) Message {
	return Message{
		Event: event,
		Data:  data,
	}
}
