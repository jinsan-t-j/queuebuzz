package requests

type AddEmailRequest struct {
	Email string `json:"email" validate:"required,email"`
}

type SetPINRequest struct {
	PIN string `json:"pin" validate:"required,len=4"`
}

type RejoinPINRequest struct {
	TicketNo string `json:"ticket_no" validate:"required"`
	PIN      string `json:"pin" validate:"required,len=4"`
}
