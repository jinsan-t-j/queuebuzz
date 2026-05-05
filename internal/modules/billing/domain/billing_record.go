package domain

import "time"

// BillingRecord is created ONLY for successful payments.
// This serves as the canonical invoice/receipt record.
type BillingRecord struct {
	ID            string    `bson:"_id" json:"id"`
	TransactionID string    `bson:"transaction_id" json:"transaction_id"`
	HostID        string    `bson:"host_id" json:"host_id"`
	PlanID        string    `bson:"plan_id" json:"plan_id"`
	PlanName      string    `bson:"plan_name" json:"plan_name"`
	BillingCycle  string    `bson:"billing_cycle" json:"billing_cycle"` // "monthly" or "yearly"
	Amount        int       `bson:"amount" json:"amount"`
	Currency      string    `bson:"currency" json:"currency"`
	InvoiceNumber string    `bson:"invoice_number" json:"invoice_number"`
	ReceiptURL    string    `bson:"receipt_url" json:"receipt_url"`
	BilledAt      time.Time `bson:"billed_at" json:"billed_at"`
}
