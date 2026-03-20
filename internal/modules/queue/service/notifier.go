package service

import (
	"encoding/json"

	"queuebuzz/internal/log"
	"queuebuzz/internal/modules/queue/domain"
	"queuebuzz/internal/modules/queue/dto"
	"queuebuzz/internal/sse"
)

// QueueNotifier publishes SSE events to queue subscribers.
// It centralises all event construction so that neither the HTTP handler
// nor the expiry service need to know about JSON marshalling or broker calls.
type QueueNotifier struct {
	broker *sse.Broker
}

// NewQueueNotifier creates a new QueueNotifier backed by the given SSE broker.
func NewQueueNotifier(broker *sse.Broker) *QueueNotifier {
	return &QueueNotifier{broker: broker}
}

// PublishEntryUpdate notifies subscribers that a new entry joined the queue.
func (n *QueueNotifier) PublishEntryUpdate(entry domain.Entry, position int) {
	n.publish(entry.QueueID, SSEMessage{
		Event: EventUserJoined,
		Data:  dto.ToEntryResponse(entry, position),
	})
}

// PublishQueueStatus notifies subscribers that the queue status changed.
func (n *QueueNotifier) PublishQueueStatus(queueID, status string) {
	n.publish(queueID, SSEMessage{
		Event: EventQueueStatusChanged,
		Data:  QueueStatusData{Status: status},
	})
}

// PublishQueueExpired notifies subscribers that a queue has expired.
func (n *QueueNotifier) PublishQueueExpired(queueID string) {
	n.publish(queueID, SSEMessage{
		Event: EventQueueExpired,
		Data:  QueueExpiredData{QueueID: queueID},
	})
}

// PublishUserCalled notifies subscribers that a user has been called.
func (n *QueueNotifier) PublishUserCalled(queueID, token, status string) {
	n.publish(queueID, SSEMessage{
		Event: EventUserCalled,
		Data:  UserStatusData{Token: token, Status: status},
	})
}

// PublishUserStatus notifies subscribers of a user status change (idle, skipped, etc.).
func (n *QueueNotifier) PublishUserStatus(queueID, token, status string) {
	n.publish(queueID, SSEMessage{
		Event: EventUserStatusChanged,
		Data:  UserStatusData{Token: token, Status: status},
	})
}

// publish marshals the message and publishes it to the broker.
func (n *QueueNotifier) publish(queueID string, msg SSEMessage) {
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Str("queue_id", queueID).Msg("SSE: failed to marshal event")
		return
	}
	n.broker.Publish(queueID, payload)
}
