package dto

type UpdateEntryRequest struct {
	Name      *string `json:"name" validate:"omitempty,min=2"`
	Email     *string `json:"email" validate:"omitempty,email"`
	Phone     *string `json:"phone" validate:"omitempty,min=8,max=15"`
	PartySize *int    `json:"party_size" validate:"omitempty,min=1,max=100"`
	FCMToken  *string `json:"fcm_token" validate:"omitempty"`
}

type SetPINRequest struct {
	PIN string `json:"pin" validate:"required,len=4"`
}

type RejoinPINRequest struct {
	TicketNo string `json:"ticket_no" validate:"required"`
	PIN      string `json:"pin" validate:"required,len=4"`
}
