package jobs

import (
	"context"
	"time"

	"queuebuzz/internal/modules/queue/service"
)

type ExpiryJob struct {
	expiryService *service.ExpiryService
}

func NewExpiryJob(s *service.ExpiryService) *ExpiryJob {
	return &ExpiryJob{
		expiryService: s,
	}
}

func (j *ExpiryJob) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			j.expiryService.SweepExpiredQueues(ctx)
			// Trigger a fallback broad broadcast in case any targeted updates failed
			j.expiryService.BroadcastAllActiveQueues(ctx)
		}
	}
}
