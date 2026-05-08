package jobs

import (
	"context"
	"time"

	"queuebuzz/internal/log"
	"queuebuzz/internal/modules/billing/service"
)

type RenewalReminderJob struct {
	billingSvc *service.BillingService
}

func NewRenewalReminderJob(svc *service.BillingService) *RenewalReminderJob {
	return &RenewalReminderJob{
		billingSvc: svc,
	}
}

func (j *RenewalReminderJob) Run(ctx context.Context) error {
	return j.billingSvc.RunRenewalReminders(ctx)
}

func (j *RenewalReminderJob) Start(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	_ = j.Run(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := j.billingSvc.RunRenewalReminders(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to run renewal reminders job")
			}
		}
	}
}
