package dto

import (
	"time"

	billingdomain "queuebuzz/internal/modules/billing/domain"
)

type PlanResponse struct {
	Plan *billingdomain.Plan `json:"plan"`
}

type PlansResponse struct {
	Plans []billingdomain.Plan `json:"plans"`
}

type CheckoutResponse struct {
	URL string `json:"url"`
}

type SubscriptionResponse struct {
	PlanName          string    `json:"plan_name"`
	Tier              string    `json:"tier"`
	Status            string    `json:"status"`
	BillingCycle      string    `json:"billing_cycle"`
	CurrentPeriodEnd  time.Time `json:"current_period_end"`
	CancelAtPeriodEnd bool      `json:"cancel_at_period_end"`
	CardLast4         string    `json:"card_last_4"`
	CanCustomBranding bool      `json:"can_custom_branding"`
	CanExportData     bool      `json:"can_export_data"`
	CanViewHistory    bool      `json:"can_view_history"`
	UpdatedAt         time.Time `json:"updated_at"`
}
