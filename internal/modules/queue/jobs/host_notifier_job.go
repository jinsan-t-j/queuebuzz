package jobs

import (
	"context"
	"fmt"
	"time"

	"queuebuzz/internal/config"
	"queuebuzz/internal/modules/queue/dto"
	"queuebuzz/internal/modules/queue/service"
)

type HostNotifyAction string

const (
	ActionUserJoined  HostNotifyAction = "joined"
	ActionUserArrived HostNotifyAction = "arrived"
	ActionUserCalled  HostNotifyAction = "called"
	ActionUserStatus  HostNotifyAction = "status_changed"
	ActionQueueStatus HostNotifyAction = "queue_status_changed"
	ActionUserUpdated HostNotifyAction = "user_updated"
)

type HostNotifyEvent struct {
	QueueID string
	Action  HostNotifyAction
	Payload interface{}
}

type StatusPayload struct {
	ID     string
	Status string
}

type HostNotifierJob struct {
	cfg          *config.Config
	queueService *service.Service
	notifier     *service.QueueNotifier
	eventChan    chan HostNotifyEvent
}

func NewHostNotifierJob(cfg *config.Config, qs *service.Service, s *service.ExpiryService) *HostNotifierJob {
	return &HostNotifierJob{
		cfg:          cfg,
		queueService: qs,
		notifier:     s.Notifier(),
		eventChan:    make(chan HostNotifyEvent, 200),
	}
}

func (j *HostNotifierJob) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-j.eventChan:
			j.processEvent(ev)
		}
	}
}

func (j *HostNotifierJob) DispatchUserJoined(queueID string, payload dto.EntryRecord) {
	j.dispatch(queueID, ActionUserJoined, payload)
}

func (j *HostNotifierJob) DispatchUserArrived(queueID, entryID string) {
	j.dispatch(queueID, ActionUserArrived, entryID)
}

func (j *HostNotifierJob) DispatchUserCalled(queueID, entryID, status string) {
	j.dispatch(queueID, ActionUserCalled, StatusPayload{ID: entryID, Status: status})
}

func (j *HostNotifierJob) DispatchUserStatus(queueID, entryID, status string) {
	j.dispatch(queueID, ActionUserStatus, StatusPayload{ID: entryID, Status: status})
}

func (j *HostNotifierJob) DispatchQueueStatus(queueID, status string) {
	j.dispatch(queueID, ActionQueueStatus, status)
}

func (j *HostNotifierJob) DispatchUserUpdated(queueID, entryID string) {
	j.dispatch(queueID, ActionUserUpdated, entryID)
}

func (j *HostNotifierJob) dispatch(queueID string, action HostNotifyAction, payload interface{}) {
	if j.cfg.IsTesting() {
		j.processEvent(HostNotifyEvent{QueueID: queueID, Action: action, Payload: payload})
		return
	}

	select {
	case j.eventChan <- HostNotifyEvent{QueueID: queueID, Action: action, Payload: payload}:
	default:
		// Optional: log dropped event or increment a counter
	}
}

func (j *HostNotifierJob) processEvent(ev HostNotifyEvent) {
	switch ev.Action {
	case ActionUserJoined:
		switch v := ev.Payload.(type) {
		case dto.EntryRecord:
			j.notifier.PublishEntryJoined(ev.QueueID, v)
		case *dto.EntryRecord:
			j.notifier.PublishEntryJoined(ev.QueueID, *v)
		default:
			panic(fmt.Errorf("ActionUserJoined payload error: %T", ev.Payload))
		}
	case ActionUserArrived:
		if entryID, ok := ev.Payload.(string); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			entry, err := j.queueService.GetEntry(ctx, entryID)
			cancel()
			if err == nil && entry != nil {
				j.notifier.PublishUserArrived(ev.QueueID, entryID, entry.Name, entry.TicketNo)
			}
		}
	case ActionUserCalled:
		switch v := ev.Payload.(type) {
		case StatusPayload:
			j.notifier.PublishUserCalled(ev.QueueID, v.ID, v.Status)
		case *StatusPayload:
			j.notifier.PublishUserCalled(ev.QueueID, v.ID, v.Status)
		}
	case ActionUserStatus:
		switch v := ev.Payload.(type) {
		case StatusPayload:
			j.notifier.PublishUserStatus(ev.QueueID, v.ID, v.Status)
		case *StatusPayload:
			j.notifier.PublishUserStatus(ev.QueueID, v.ID, v.Status)
		}
	case ActionQueueStatus:
		if status, ok := ev.Payload.(string); ok {
			j.notifier.PublishQueueStatus(ev.QueueID, status)
		}
	case ActionUserUpdated:
		if entryID, ok := ev.Payload.(string); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			entry, err := j.queueService.GetEntry(ctx, entryID)
			pos, _ := j.queueService.GetPosition(ctx, ev.QueueID, entryID)
			cancel()
			if err == nil && entry != nil {
				j.notifier.PublishEntryUpdated(ev.QueueID, dto.ToEntryResponse(*entry, pos+1))
			}
		}
	}
}
