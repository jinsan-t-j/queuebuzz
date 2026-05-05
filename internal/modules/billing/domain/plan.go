package domain

import "time"

type PlanLimit struct {
	MaxQueuesPerMonth    int  `bson:"max_queues_per_month" json:"max_queues_per_month"`
	MaxGuestsPerQueue    int  `bson:"max_guests_per_queue" json:"max_guests_per_queue"`
	HistoryAccess        bool `bson:"history_access" json:"history_access"`
	CustomBranding       bool `bson:"custom_branding" json:"custom_branding"`
	CanExport            bool `bson:"can_export" json:"can_export"`
	QueueExpiryHours     int  `bson:"queue_expiry_hours" json:"queue_expiry_hours"`
	CanViewGuestData     bool `bson:"can_view_guest_data" json:"can_view_guest_data"`
	HistoryRetentionDays int  `bson:"history_retention_days" json:"history_retention_days"`
}

type Plan struct {
	ID                       string    `bson:"_id,omitempty" json:"id"`
	Slug                     string    `bson:"slug" json:"slug"`
	Tier                     string    `bson:"tier" json:"tier"` // e.g., "free", "pro", "elite", "enterprise"
	ProviderMonthlyProductID string    `bson:"provider_monthly_product_id,omitempty" json:"-"`
	ProviderYearlyProductID  string    `bson:"provider_yearly_product_id,omitempty" json:"-"`
	Priority                 int       `bson:"priority" json:"priority"`
	Name                     string    `bson:"name" json:"name"`
	Description              string    `bson:"description" json:"description"`
	IsFree                   bool      `bson:"is_free" json:"is_free"`
	CountryCode              string    `bson:"country_code" json:"country_code"` // e.g., "IN", "US", or "GLOBAL"
	Currency                 string    `bson:"currency" json:"currency"`         // e.g., "INR", "USD"
	MonthlyPrice             int       `bson:"monthly_price" json:"monthly_price"`
	YearlyPrice              int       `bson:"yearly_price" json:"yearly_price"`
	Limits                   PlanLimit `bson:"limits" json:"limits"`
	CreatedAt                time.Time `bson:"created_at" json:"created_at"`
}
