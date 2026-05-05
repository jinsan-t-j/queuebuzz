package domain

import "time"

type TransactionStatus string

const (
	TransactionPending   TransactionStatus = "pending"
	TransactionSucceeded TransactionStatus = "succeeded"
	TransactionFailed    TransactionStatus = "failed"
)

// Transaction records every payment attempt (success AND failure).
// Only payment.succeeded / payment.failed webhooks create transactions.
type Transaction struct {
	ID                 string            `bson:"_id" json:"id"`
	HostID             string            `bson:"host_id" json:"host_id"`
	PlanID             string            `bson:"plan_id" json:"plan_id"`
	BillingCycle       string            `bson:"billing_cycle" json:"billing_cycle"` // "monthly" or "yearly"
	Amount             int               `bson:"amount" json:"amount"`
	TotalAmount        int               `bson:"total_amount" json:"total_amount"` // total_amount from Dodo (includes tax)
	Currency           string            `bson:"currency" json:"currency"`
	Status             TransactionStatus `bson:"status" json:"status"`
	Provider           string            `bson:"provider" json:"provider"`
	ProviderPaymentID  string            `bson:"provider_payment_id" json:"provider_payment_id"`
	ProviderCustomerID string            `bson:"provider_customer_id,omitempty" json:"provider_customer_id,omitempty"`
	FailureReason      string            `bson:"failure_reason,omitempty" json:"failure_reason,omitempty"`
	InvoiceURL         string            `bson:"invoice_url,omitempty" json:"invoice_url,omitempty"`
	CardLast4          string            `bson:"card_last4,omitempty" json:"card_last4,omitempty"`
	CardBrand          string            `bson:"card_brand,omitempty" json:"card_brand,omitempty"`
	CardExpiry         string            `bson:"card_expiry,omitempty" json:"card_expiry,omitempty"`
	CreatedAt          time.Time         `bson:"created_at" json:"created_at"`
}
