package service

import (
	"encoding/json"

	"queuebuzz/internal/log"
	"queuebuzz/internal/modules/queue/dto"
	"queuebuzz/internal/modules/queue/events"
	"queuebuzz/internal/sse"
)

// QueueNotifier publishes SSE events to queue and entry topics.
type QueueNotifier struct {
	broker *sse.Broker
}

func NewQueueNotifier(broker *sse.Broker) *QueueNotifier {
	return &QueueNotifier{broker: broker}
}

// entryTopic returns the per-entry SSE topic key ("entry:{id}").
func entryTopic(entryID string) string { return "entry:" + entryID }

// PublishEntryUpdate notifies the host that a user joined the queue.
func (n *QueueNotifier) PublishEntryUpdate(queueID string, entry dto.EntryRecord) {
	n.publish(queueID, events.Wrap(events.EventUserJoined, entry))
}

func (n *QueueNotifier) PublishQueueStatus(queueID, status string) {
	n.publish(queueID, events.Wrap(events.EventQueueStatusChanged, events.QueueStatusData{Status: status}))
}

func (n *QueueNotifier) PublishQueueExpired(queueID string) {
	n.publish(queueID, events.Wrap(events.EventQueueExpired, events.QueueExpiredData{QueueID: queueID}))
}

func (n *QueueNotifier) PublishUserCalled(queueID, token, status string) {
	n.publish(queueID, events.Wrap(events.EventUserCalled, events.UserStatusData{Token: token, Status: status}))
}

func (n *QueueNotifier) PublishUserStatus(queueID, token, status string) {
	n.publish(queueID, events.Wrap(events.EventUserStatusChanged, events.UserStatusData{Token: token, Status: status}))
}

// PublishPositionUpdates broadcasts positions to all waiting entries.
// It takes a list of IDs to minimize memory usage.
func (n *QueueNotifier) PublishPositionUpdates(entryIDs []string) {
	for i, id := range entryIDs {
		n.publish(entryTopic(id), events.Wrap(events.EventPositionUpdate, events.PositionUpdateData{Position: int64(i + 1)}))
	}
}

// PublishEntryStatusChanged pushed an individual status change.
func (n *QueueNotifier) PublishEntryStatusChanged(entryID, status string) {
	n.publish(entryTopic(entryID), events.Wrap(events.EventEntryStatusChanged, events.EntryStatusChangedData{Status: status}))
}

func (n *QueueNotifier) publish(topic string, msg sse.Message) {
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Str("topic", topic).Msg("SSE: failed to marshal event")
		return
	}
	n.broker.Publish(topic, payload)
}
