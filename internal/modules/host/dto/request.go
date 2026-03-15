package dto

type RegisterRequest struct {
	Email *string `json:"email" validate:"omitempty,email"`
	Phone *string `json:"phone" validate:"omitempty,e164"`
}
