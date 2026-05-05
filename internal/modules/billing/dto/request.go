package dto

type CheckoutRequest struct {
	PlanID       string `json:"plan_id" validate:"required"`
	BillingCycle string `json:"billing_cycle" validate:"oneof=monthly yearly"`
}

type WebhookRequest struct {
	Type string `json:"type" validate:"required"`
	Data any    `json:"data" validate:"required"`
}

type CancelSubscriptionRequest struct {
	Comment  *string `json:"comment"`
	Feedback *string `json:"feedback"`
}

type UpdatePaymentMethodRequest struct {
	ReturnURL string `json:"return_url"`
}
