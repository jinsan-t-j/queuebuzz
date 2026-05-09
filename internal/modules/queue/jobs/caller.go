package jobs

import (
	"context"

	"queuebuzz/internal/config"
	"queuebuzz/internal/modules/queue/service"
)

type CallTask struct {
	QueueID string
	EntryID string
	Status  string
}

type Caller struct {
	notifier *service.QueueNotifier
	callChan chan CallTask
}

func NewCaller(n *service.QueueNotifier) *Caller {
	return &Caller{
		notifier: n,
		callChan: make(chan CallTask, 100),
	}
}

func (c *Caller) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-c.callChan:
			c.notifier.PublishUserCalled(task.QueueID, task.EntryID, task.Status)
		}
	}
}

func (c *Caller) DispatchCall(queueID, entryID, status string) {
	if config.Get().IsTesting() {
		c.notifier.PublishUserCalled(queueID, entryID, status)
		return
	}

	select {
	case c.callChan <- CallTask{QueueID: queueID, EntryID: entryID, Status: status}:
	default:
		// Drop if buffer is full; this is a best-effort notification system
	}
}
