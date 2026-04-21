package dto

type CreateQueueRequest struct {
	Name              string  `json:"name" validate:"required,min=3,max=50"`
	Slug              *string `json:"slug" validate:"omitempty,min=3,max=20,slug"`
	AvgServiceMins    *int    `json:"avg_service_mins" validate:"omitempty,min=1,max=60"`
	AllowPartyJoining *bool   `json:"allow_party_joining" validate:"omitempty"`
	MaxPartySize      *int    `json:"max_party_size" validate:"omitempty,min=1,max=100"`
	RecoveryEmail     *string `json:"recovery_email" validate:"omitempty,email"`
}

type UpdateQueueRequest struct {
	Name              *string `json:"name" validate:"omitempty,min=3,max=50"`
	AvgServiceMins    *int    `json:"avg_service_mins" validate:"omitempty,min=1,max=60"`
	RecoveryEmail     *string `json:"recovery_email" validate:"omitempty,email"`
	Slug              *string `json:"slug" validate:"omitempty,min=3,max=20,slug"`
	AllowPartyJoining *bool   `json:"allow_party_joining" validate:"omitempty"`
	MaxPartySize      *int    `json:"max_party_size" validate:"omitempty,min=1,max=100"`
	StrictQueueMode   *bool   `json:"strict_queue_mode" validate:"omitempty"`
	Notes             *string `json:"notes" validate:"omitempty,max=500"`
}

type JoinByCodeRequest struct {
	JoinCode    string  `json:"join_code" validate:"required,len=6"`
	FCMToken    *string `json:"fcm_token" validate:"omitempty"`
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email" validate:"omitempty,email"`
	Phone       *string `json:"phone" validate:"omitempty,len=10"`
	PartySize   *int    `json:"party_size" validate:"omitempty,min=1,max=100"`
	PIN         *string `json:"pin" validate:"omitempty,len=4"`
}

type JoinRequest struct {
	FCMToken    *string `json:"fcm_token" validate:"omitempty"`
	DisplayName *string `json:"display_name"`
	Email       *string `json:"email" validate:"omitempty,email"`
	Phone       *string `json:"phone" validate:"omitempty,len=10"`
	PartySize   *int    `json:"party_size" validate:"omitempty,min=1,max=100"`
	PIN         *string `json:"pin" validate:"omitempty,len=4"`
}

type AddEntryRequest struct {
	Name      string  `json:"name" validate:"required,min=3,max=50"`
	Email     *string `json:"email" validate:"omitempty,email"`
	Phone     *string `json:"phone" validate:"omitempty,len=10"`
	PartySize *int    `json:"party_size" validate:"omitempty,min=1,max=100"`
}
