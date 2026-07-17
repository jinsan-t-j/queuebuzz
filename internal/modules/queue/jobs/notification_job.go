package jobs

import (
	"context"
	"sync"
	"time"

	"queuebuzz/internal/log"
)

// NotificationJob is a bounded worker pool for FCM push and email
// notifications. It replaces fire-and-forget `go run()` goroutines
// with a fixed pool that provides:
//   - Backpressure (bounded channel, drops on full instead of OOM)
//   - Graceful shutdown (WaitGroup-tracked workers drain in-flight tasks)
//   - Memory control (fixed goroutine count)
//   - Panic recovery per task
type NotificationJob struct {
	ch      chan notifyItem
	workers int
	wg      sync.WaitGroup
}

type notifyItem struct {
	name string
	fn   func(ctx context.Context)
}

// NewNotificationJob creates a job with the given channel buffer and worker count.
func NewNotificationJob(bufSize, workers int) *NotificationJob {
	return &NotificationJob{
		ch:      make(chan notifyItem, bufSize),
		workers: workers,
	}
}

// Start launches the worker pool. Blocks until ctx is cancelled.
func (j *NotificationJob) Start(ctx context.Context) {
	for i := 0; i < j.workers; i++ {
		j.wg.Add(1)
		go j.worker(ctx, i)
	}

	<-ctx.Done()
	close(j.ch)
	j.wg.Wait()

	log.Info().Msg("NotificationJob: all workers stopped")
}

// DispatchFunc enqueues a named function for background execution.
// Non-blocking; drops on full.
func (j *NotificationJob) DispatchFunc(name string, fn func(ctx context.Context)) {
	select {
	case j.ch <- notifyItem{name: name, fn: fn}:
	default:
		log.Warn().Str("task", name).Msg("NotificationJob: channel full, task dropped")
	}
}

func (j *NotificationJob) worker(ctx context.Context, id int) {
	defer j.wg.Done()

	for item := range j.ch {
		func() {
			taskCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()

			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Str("task", item.name).
						Int("worker", id).Msg("NotificationJob: worker recovered from panic")
				}
			}()

			item.fn(taskCtx)
		}()
	}
}
