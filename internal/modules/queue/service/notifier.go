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

// pubTopic returns the public SSE topic key ("queue_public:{id}").
func pubTopic(queueID string) string { return "queue_public:" + queueID }

// PublishEntryUpdate notifies the host that a user joined the queue.
func (n *QueueNotifier) PublishEntryUpdate(queueID string, entry dto.EntryRecord) {
	n.publish(queueID, events.Wrap(sse.NewMessage(events.EventUserJoined, entry)))
}

func (n *QueueNotifier) PublishQueueStatus(queueID, status string) {
	msg := events.Wrap(sse.NewMessage(events.EventQueueStatusChanged, events.QueueStatusData{Status: status}))
	n.publish(queueID, msg)
	n.publish(pubTopic(queueID), msg)
}

func (n *QueueNotifier) PublishUserCalled(queueID, entryID, status string) {
	n.publish(queueID, events.Wrap(sse.NewMessage(events.EventUserCalled, events.UserStatusData{ID: entryID, Status: status})))
	n.PublishEntryStatusChanged(entryID, status)
}

func (n *QueueNotifier) PublishUserStatus(queueID, entryID, status string) {
	n.publish(queueID, events.Wrap(sse.NewMessage(events.EventUserStatusChanged, events.UserStatusData{ID: entryID, Status: status})))
	n.PublishEntryStatusChanged(entryID, status)
}

func (n *QueueNotifier) PublishUserArrived(queueID, entryID, name, ticketNo string) {
	n.publish(queueID, events.Wrap(sse.NewMessage(events.EventUserArrived, events.UserArrivedData{
		ID:           entryID,
		Name:         name,
		TicketNumber: ticketNo,
	})))
}

// PublishPositionUpdates broadcasts positions to all waiting entries.
// It takes a list of IDs to minimize memory usage.
func (n *QueueNotifier) PublishPositionUpdates(entryIDs []string) {
	for i, id := range entryIDs {
		n.publish(entryTopic(id), events.Wrap(sse.NewMessage(events.EventPositionUpdate, events.PositionUpdateData{Position: int64(i + 1)})))
	}
}

// PublishEntryStatusChanged pushed an individual status change.
func (n *QueueNotifier) PublishEntryStatusChanged(entryID, status string) {
	n.publish(entryTopic(entryID), events.Wrap(sse.NewMessage(events.EventEntryStatusChanged, events.EntryStatusChangedData{Status: status})))
}

func (n *QueueNotifier) PublishWaitingCount(queueID string, count int64) {
	n.publish(pubTopic(queueID), events.Wrap(sse.NewMessage("waiting_count_updated", map[string]interface{}{"count": count})))
}

func (n *QueueNotifier) publish(topic string, msg sse.Message) {
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Str("topic", topic).Msg("SSE: failed to marshal event")
		return
	}
	n.broker.Publish(topic, payload)
}
