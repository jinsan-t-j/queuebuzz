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

func (j *ExpiryJob) Run(ctx context.Context) error {
	j.expiryService.SweepExpiredQueues(ctx)
	// Trigger a fallback broad broadcast in case any targeted updates failed
	j.expiryService.BroadcastAllActiveQueues(ctx)
	return nil
}

func (j *ExpiryJob) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	_ = j.Run(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = j.Run(ctx)
		}
	}
}
