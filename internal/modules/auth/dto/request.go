package dto

type RegisterRequest struct {
	Email *string `json:"email" validate:"omitempty,email"`
	Phone *string `json:"phone" validate:"omitempty,e164"`
}

type VerifyRequest struct {
	Token *string `json:"token" validate:"omitempty"`
	Phone *string `json:"phone" validate:"omitempty,e164"`
	OTP   *string `json:"otp" validate:"omitempty,len=6"`
}

type SocialAuthRequest struct {
	Provider string `json:"provider" validate:"required,oneof=google apple"`
}

type CheckMethodRequest struct {
	Email string `json:"email" validate:"required,email"`
}
