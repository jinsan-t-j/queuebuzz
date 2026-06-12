package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/constants"
	"queuebuzz/internal/firebase"
	"queuebuzz/internal/log"
	customerevents "queuebuzz/internal/modules/customer/events"
	"queuebuzz/internal/modules/queue/domain"
	"queuebuzz/internal/modules/queue/dto"
	"queuebuzz/internal/modules/queue/events"
	"queuebuzz/internal/services"
	"queuebuzz/internal/sse"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

// QueueNotifier publishes SSE events to queue and entry topics.
type QueueNotifier struct {
	ctx      context.Context
	broker   *sse.Broker
	fb       firebase.NotificationSender
	emailSvc *services.EmailService
	queueCol *mongodriver.Collection
	entryCol *mongodriver.Collection
	appURL   string
}

func NewQueueNotifier(
	ctx context.Context,
	cfg *config.Config,
	broker *sse.Broker,
	fb firebase.NotificationSender,
	emailSvc *services.EmailService,
	queueCol *mongodriver.Collection,
	entryCol *mongodriver.Collection,
) *QueueNotifier {
	return &QueueNotifier{
		ctx:      ctx,
		broker:   broker,
		fb:       fb,
		emailSvc: emailSvc,
		queueCol: queueCol,
		entryCol: entryCol,
		appURL:   strings.TrimRight(cfg.AppURL, "/"),
	}
}

// entryTopic returns the per-entry SSE topic key ("entry:{id}").
func entryTopic(entryID string) string { return "entry:" + entryID }

// pubTopic returns the public SSE topic key ("queue_public:{id}").
func pubTopic(queueID string) string { return "queue_public:" + queueID }

// PublishEntryJoined notifies the host that a user joined the queue.
func (n *QueueNotifier) PublishEntryJoined(queueID string, entry dto.EntryRecord) {
	n.publish(queueID, events.Wrap(sse.NewMessage(events.EventUserJoined, entry)))

	// Strip PII for public channel
	publicEntry := entry
	publicEntry.Email = nil
	publicEntry.Phone = nil

	// Check manual positioning to conditionally hide position
	var q domain.Queue
	dbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := n.queueCol.FindOne(dbCtx, bson.M{"_id": queueID}).Decode(&q); err == nil && q.ManualPositioning {
		publicEntry.Position = 0
	}

	n.publish(pubTopic(queueID), events.Wrap(sse.NewMessage(events.EventUserJoined, publicEntry)))

	// Notify Host
	n.notifyHost(queueID, "New Guest Joined", fmt.Sprintf("%s is now waiting with ticket %s", entry.Name, entry.TicketNo), map[string]string{
		"event":    events.EventUserJoined,
		"queue_id": queueID,
		"entry_id": entry.ID,
	})
}

// PublishEntryUpdated notifies the host and public channel that a user's details were updated.
func (n *QueueNotifier) PublishEntryUpdated(queueID string, entry dto.EntryRecord) {
	n.publish(queueID, events.Wrap(sse.NewMessage(events.EventUserUpdated, entry)))

	// Strip PII for public channel
	publicEntry := entry
	publicEntry.Email = nil
	publicEntry.Phone = nil

	// Check manual positioning to conditionally hide position
	var q domain.Queue
	dbCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := n.queueCol.FindOne(dbCtx, bson.M{"_id": queueID}).Decode(&q); err == nil && q.ManualPositioning {
		publicEntry.Position = 0
	}

	n.publish(pubTopic(queueID), events.Wrap(sse.NewMessage(events.EventUserUpdated, publicEntry)))
}

// PublishQueueStatus broadcasts a general queue state change to the Host and Public viewers.
// Use this for broad state changes (ACTIVE, PAUSED, CLOSED).
// It updates the Host Dashboard and the Public Join page.
func (n *QueueNotifier) PublishQueueStatus(queueID, status string) {
	msg := events.Wrap(sse.NewMessage(events.EventQueueStatusChanged, events.QueueStatusData{Status: status}))
	n.publish(queueID, msg)
	n.publish(pubTopic(queueID), msg)

	// Notify Host on important status changes (if any external system or job changes it)
	// Usually host changes it themselves, but for consistency:
	n.notifyHost(queueID, "Queue Status Updated", fmt.Sprintf("Queue is now %s", status), map[string]string{
		"event":    events.EventQueueStatusChanged,
		"status":   status,
		"queue_id": queueID,
	})
}

func (n *QueueNotifier) PublishUserCalled(queueID, entryID, status string) {
	msg := events.Wrap(sse.NewMessage(events.EventUserCalled, events.UserStatusData{ID: entryID, Status: status}))
	n.publish(queueID, msg)
	n.publish(pubTopic(queueID), msg)
	n.PublishEntryStatusChanged(entryID, status)

	// Notify Guest
	n.notifyEntry(entryID, "It's your turn!", "Please head to the counter now.", map[string]string{
		"event":    events.EventUserCalled,
		"queue_id": queueID,
		"entry_id": entryID,
	})
}

// PublishUserStatus broadcasts a status update for a specific entry to the Host Dashboard on user left and Public Join page.
func (n *QueueNotifier) PublishUserStatus(queueID, entryID, status string) {
	msg := events.Wrap(sse.NewMessage(events.EventUserStatusChanged, events.UserStatusData{ID: entryID, Status: status}))
	n.publish(queueID, msg)
	n.publish(pubTopic(queueID), msg)
	n.PublishEntryStatusChanged(entryID, status)

	// If user left, notify host
	if status == constants.EntryStatusLeft {
		name := "A guest"
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var entry domain.Entry
		if err := n.entryCol.FindOne(ctx, bson.M{"$or": []bson.M{{"_id": entryID}, {"token": entryID}}}).Decode(&entry); err == nil {
			name = entry.Name
		}
		cancel()

		n.notifyHost(queueID, "Guest Left Queue", fmt.Sprintf("%s has removed themselves from the queue.", name), map[string]string{
			"event":    "user_left",
			"queue_id": queueID,
			"entry_id": entryID,
		})
	}
}

func (n *QueueNotifier) PublishUserArrived(queueID, entryID, name, ticketNo string) {
	msg := events.Wrap(sse.NewMessage(events.EventUserArrived, events.UserArrivedData{
		ID:           entryID,
		Name:         name,
		TicketNumber: ticketNo,
	}))
	n.publish(queueID, msg)
	n.publish(pubTopic(queueID), msg)

	// Notify Host
	n.notifyHost(queueID, "Guest Arrived!", fmt.Sprintf("%s (%s) has arrived.", name, ticketNo), map[string]string{
		"event":    events.EventUserArrived,
		"queue_id": queueID,
		"entry_id": entryID,
	})
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

func (n *QueueNotifier) PublishWaitingCount(queueID string, count int64, avgServiceMins int) {
	n.publish(pubTopic(queueID), events.Wrap(sse.NewMessage("waiting_count_updated", map[string]interface{}{
		"count":            count,
		"avg_service_mins": avgServiceMins,
	})))
}

// NotifyQueueEnded provides a targeted teardown for an individual unserved guest.
// It triggers Push/Email alerts. The UI redirection is handled by the global
// PublishQueueStatus signal received via the public SSE stream.
func (n *QueueNotifier) NotifyQueueEnded(entryID, queueName string) {
	// 1. Send External Notifications (Push/Email) - pocket buzz is still needed!
	n.notifyEntry(entryID, "Queue Ended", fmt.Sprintf("The host has closed the session for %s. We've skipped your entry.", queueName), map[string]string{
		"event":      events.EventQueueEnded,
		"queue_name": queueName,
	})
}

// NotifyHeadsUp sends a "get ready" alert to the next-in-line waiting guests.
// Called after a guest is buzzed so positions 2 and 3 know they're almost up.
func (n *QueueNotifier) NotifyHeadsUp(queueID string, entryIDs []string) {
	titles := []string{
		"You're next!",
		"Almost your turn!",
	}
	bodies := []string{
		"Get ready — you're second in line. Start heading over now.",
		"Heads up — you're third in line. Your turn is coming up soon.",
	}

	for i, id := range entryIDs {
		if i >= 2 {
			break
		}
		pos := i + 2 // position 2 or 3

		// SSE event so the UI can react in real time
		n.publish(entryTopic(id), events.Wrap(sse.NewMessage(events.EventHeadsUp, events.HeadsUpData{Position: pos})))

		// Push notification
		n.notifyEntry(id, titles[i], bodies[i], map[string]string{
			"event":    events.EventHeadsUp,
			"queue_id": queueID,
			"entry_id": id,
		})
	}
}

func (n *QueueNotifier) publish(topic string, msg sse.Message) {
	payload, err := json.Marshal(msg)
	if err != nil {
		log.Error().Err(err).Str("topic", topic).Msg("SSE: failed to marshal event")
		return
	}
	n.broker.Publish(topic, payload)
}

func (n *QueueNotifier) absoluteURL(path string) string {
	if path == "" {
		return n.appURL
	}

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	return n.appURL + path
}

func (n *QueueNotifier) notifyHost(queueID, title, body string, data map[string]string) {
	// Fiber v3 strings are reused; must clone before goroutine
	queueID = strings.Clone(queueID)
	title = strings.Clone(title)
	body = strings.Clone(body)
	safeData := make(map[string]string, len(data))
	for k, v := range data {
		safeData[strings.Clone(k)] = strings.Clone(v)
	}

	run := func() {
		ctx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
		defer cancel()

		var q domain.Queue
		if err := n.queueCol.FindOne(ctx, bson.M{"_id": queueID}).Decode(&q); err != nil {
			log.Warn().Err(err).Str("queue_id", queueID).Msg("FCM: skipping host notification, queue not found")
			return
		}

		if q.HostFCMToken == nil || *q.HostFCMToken == "" {
			log.Debug().Str("queue_id", queueID).Msg("FCM: skipping host notification, no host token registered")
			return
		}

		hostLink := fmt.Sprintf("/guest-host/queue/%s/live", queueID)
		if q.HostID != nil && *q.HostID != "" {
			hostLink = "/dashboard"
		}

		payload := map[string]string{
			"title": title,
			"body":  body,
			"link":  n.absoluteURL(hostLink),
		}
		for key, value := range safeData {
			payload[key] = value
		}

		if err := n.fb.SendToUser(ctx, *q.HostFCMToken, title, body, payload); err != nil {
			if firebase.IsTokenInvalid(err) {
				_, _ = n.queueCol.UpdateOne(ctx, bson.M{"_id": queueID}, bson.M{
					"$set": bson.M{
						"host_fcm_token": "",
					},
					"$unset": bson.M{
						"host_fcm_updated_at": "",
					},
				})
			}
		}
	}

	if config.Get().IsTesting() {
		run()
	} else {
		go run()
	}
}

func (n *QueueNotifier) notifyEntry(entryID, title, body string, data map[string]string) {
	// Fiber v3 strings are reused; must clone before goroutine
	entryID = strings.Clone(entryID)
	title = strings.Clone(title)
	body = strings.Clone(body)
	safeData := make(map[string]string, len(data))
	for k, v := range data {
		safeData[strings.Clone(k)] = strings.Clone(v)
	}

	run := func() {
		ctx, cancel := context.WithTimeout(n.ctx, 15*time.Second)
		defer cancel()

		var e domain.Entry
		if err := n.entryCol.FindOne(ctx, bson.M{"_id": entryID}).Decode(&e); err != nil {
			log.Warn().Err(err).Str("entry_id", entryID).Msg("FCM: skipping guest notification, entry not found")
			return
		}

		// Dispatch Email Fallback if enabled and guest has an email
		// We do this for "User Called" (Buzz) and "Queue Ended" events
		event := safeData["event"]
		isBuzz := event == events.EventUserCalled
		isEnd := event == events.EventQueueEnded
		hasEmail := e.Email != nil && *e.Email != ""

		fcmToken := ""
		if e.FCMToken != nil {
			fcmToken = *e.FCMToken
		}

		if fcmToken == "" {
			if isBuzz && hasEmail {
				_ = n.emailSvc.SendBuzzFallback(*e.Email, e.TicketNo, "Your Queue", e.QueueID)
			} else if isEnd && hasEmail {
				_ = n.emailSvc.SendQueueEnded(*e.Email, safeData["queue_name"])
			}
			log.Debug().Str("entry_id", entryID).Msg("FCM: skipping guest notification, no entry token registered")
			return
		}

		payload := map[string]string{
			"title": title,
			"body":  body,
		}
		for key, value := range safeData {
			payload[key] = value
		}

		if queueID, ok := payload["queue_id"]; ok && queueID != "" {
			payload["link"] = n.absoluteURL(fmt.Sprintf("/q/%s/waiting", queueID))
		}

		if err := n.fb.SendToUser(ctx, fcmToken, title, body, payload); err != nil {
			if isBuzz && hasEmail {
				_ = n.emailSvc.SendBuzzFallback(*e.Email, e.TicketNo, "Your Queue", e.QueueID)
			} else if isEnd && hasEmail {
				_ = n.emailSvc.SendQueueEnded(*e.Email, safeData["queue_name"])
			}

			if firebase.IsTokenInvalid(err) {
				_, _ = n.entryCol.UpdateOne(ctx, bson.M{"_id": entryID}, bson.M{
					"$unset": bson.M{
						"fcm_token": "",
					},
				})
				n.publish(entryTopic(entryID), customerevents.Wrap(customerevents.EventPushTokenRefresh, customerevents.PushTokenRefreshData{
					Reason: "invalid_token",
				}))
			}
		}
	}

	if config.Get().IsTesting() {
		run()
	} else {
		go run()
	}
}
