package domain

import "time"

type SubscriptionStatus string

const (
	SubscriptionActive   SubscriptionStatus = "active"
	SubscriptionPastDue  SubscriptionStatus = "past_due"
	SubscriptionCanceled SubscriptionStatus = "canceled"
	SubscriptionInGrace  SubscriptionStatus = "in_grace"
	SubscriptionPending  SubscriptionStatus = "pending"
)

type Subscription struct {
	ID                     string             `bson:"_id" json:"id"`
	HostID                 string             `bson:"host_id" json:"host_id"`
	PlanID                 string             `bson:"plan_id" json:"plan_id"`
	Status                 SubscriptionStatus `bson:"status" json:"status"`
	ProviderSubscriptionID string             `bson:"provider_subscription_id,omitempty" json:"provider_subscription_id,omitempty"`
	ProviderCustomerID     string             `bson:"provider_customer_id,omitempty" json:"provider_customer_id,omitempty"`
	BillingCycle           string             `bson:"billing_cycle,omitempty" json:"billing_cycle,omitempty"`
	GracePeriodUntil       *time.Time         `bson:"grace_period_until,omitempty" json:"grace_period_until,omitempty"`
	CurrentPeriodEnd       time.Time          `bson:"current_period_end" json:"current_period_end"`
	CancelAtPeriodEnd      bool               `bson:"cancel_at_period_end" json:"cancel_at_period_end"`
	CancellationComment    string             `bson:"cancellation_comment,omitempty" json:"cancellation_comment,omitempty"`
	CancellationFeedback   string             `bson:"cancellation_feedback,omitempty" json:"cancellation_feedback,omitempty"`
	CancelledAt            *time.Time         `bson:"cancelled_at,omitempty" json:"cancelled_at,omitempty"`
	CardLast4              string             `bson:"card_last4,omitempty" json:"card_last4,omitempty"`
	CardBrand              string             `bson:"card_brand,omitempty" json:"card_brand,omitempty"`
	CardExpiry             string             `bson:"card_expiry,omitempty" json:"card_expiry,omitempty"`
	RenewalReminderSentAt  *time.Time         `bson:"renewal_reminder_sent_at,omitempty" json:"renewal_reminder_sent_at,omitempty"`
	CreatedAt              time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt              time.Time          `bson:"updated_at" json:"updated_at"`
}
