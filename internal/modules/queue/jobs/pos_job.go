package jobs

import (
	"context"
	"queuebuzz/internal/modules/queue/service"
)

// PositionJob handles heavy recalculation tasks for the entire queue.
type PositionJob struct {
	expiryService *service.ExpiryService
	updateChan    chan string
}

func NewPositionJob(s *service.ExpiryService) *PositionJob {
	return &PositionJob{
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
	select {
	case j.updateChan <- queueID:
	default:
	}
}
