package dto

type RegisterRequest struct {
	Email *string `json:"email" validate:"omitempty,email"`
	Phone *string `json:"phone" validate:"omitempty,e164"`
}

type UpdateMeRequest struct {
	Name     *string                `json:"name,omitempty"`
	Email    *string                `json:"email,omitempty"`
	Settings map[string]interface{} `json:"settings,omitempty"`
}
