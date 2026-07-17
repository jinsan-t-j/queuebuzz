package jobs

import (
	"context"
	"queuebuzz/internal/config"
	"queuebuzz/internal/modules/queue/service"
)

// PositionJob handles heavy recalculation tasks for the entire queue.
type PositionJob struct {
	cfg           *config.Config
	expiryService *service.ExpiryService
	updateChan    chan string
}

func NewPositionJob(cfg *config.Config, s *service.ExpiryService) *PositionJob {
	return &PositionJob{
		cfg:           cfg,
		expiryService: s,
		updateChan:    make(chan string, 100),
	}
}

func (j *PositionJob) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case queueID := <-j.updateChan:
			j.expiryService.BroadcastPositionsForQueue(ctx, queueID, 0)
		}
	}
}

func (j *PositionJob) Dispatch(queueID string) {
	if j.cfg.IsTesting() {
		j.expiryService.BroadcastPositionsForQueue(context.Background(), queueID, 0)
		return
	}

	select {
	case j.updateChan <- queueID:
	default:
	}
}
