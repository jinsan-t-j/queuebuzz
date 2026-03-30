package jobs

import (
	"context"

	"queuebuzz/internal/modules/queue/dto"
	"queuebuzz/internal/modules/queue/service"
)

type EntryUpdateTask struct {
	QueueID string
	Payload interface{}
}

type Broadcaster struct {
	expiryService   *service.ExpiryService
	posUpdateChan   chan string
	entryUpdateChan chan EntryUpdateTask
}

func NewBroadcaster(s *service.ExpiryService) *Broadcaster {
	return &Broadcaster{
		expiryService:   s,
		posUpdateChan:   make(chan string, 100),
		entryUpdateChan: make(chan EntryUpdateTask, 100),
	}
}

func (b *Broadcaster) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case queueID := <-b.posUpdateChan:
			b.expiryService.BroadcastPositionsForQueue(ctx, queueID)
		case task := <-b.entryUpdateChan:
			if rec, ok := task.Payload.(dto.EntryRecord); ok {
				b.expiryService.Notifier().PublishEntryUpdate(task.QueueID, rec)
			}
		}
	}
}

func (b *Broadcaster) DispatchPositionUpdate(queueID string) {
	select {
	case b.posUpdateChan <- queueID:
	default:
		// Drop if buffer is full; the next dispatch will catch up or cron will fallback
	}
}

func (b *Broadcaster) DispatchEntryUpdate(queueID string, payload interface{}) {
	select {
	case b.entryUpdateChan <- EntryUpdateTask{QueueID: queueID, Payload: payload}:
	default:
	}
}
