package domain

type SystemSettings struct {
	ID            string `bson:"_id,omitempty" json:"id"`
	Slug          string `bson:"slug" json:"slug"`
	DefaultPlanID string `bson:"default_plan_id" json:"default_plan_id"`

	// Queue Behavior Defaults (Global Tuning)
	DefaultQueueJoinCodeLength      int `bson:"default_queue_join_code_length" json:"default_queue_join_code_length"`
	DefaultQueueMaxJoinCodeAttempts int `bson:"default_queue_max_join_code_attempts" json:"default_queue_max_join_code_attempts"`

	// Entry Behavior Defaults (Guest Tuning)
	DefaultQueueEntryIdleTimeoutMin   int `bson:"default_queue_entry_idle_timeout_min" json:"default_queue_entry_idle_timeout_min"`
	DefaultQueueEntryGraceTimerSec    int `bson:"default_queue_entry_grace_timer_sec" json:"default_queue_entry_grace_timer_sec"`
	DefaultQueueEntryRepositionOffset int `bson:"default_queue_entry_reposition_offset" json:"default_queue_entry_reposition_offset"`

	// Billing Logic
	DefaultQueueSubscriptionGracePeriodDays int    `bson:"default_queue_subscription_grace_period_days" json:"default_queue_subscription_grace_period_days"`
	SupportEmail                            string `bson:"support_email" json:"support_email"`

	// Security & Validation
	AllowedEmailDomains []string `bson:"allowed_email_domains" json:"allowed_email_domains"`
}
