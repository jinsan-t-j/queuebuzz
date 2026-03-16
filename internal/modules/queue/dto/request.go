package dto

type CreateQueueRequest struct {
	QueueName      string `json:"queue_name" validate:"required,min=3,max=50"`
	Slug           string `json:"slug" validate:"omitempty,min=3,max=20,alphanum"`
	AvgServiceMins int    `json:"avg_service_mins" validate:"omitempty,min=1,max=60"`
}

type JoinByCodeRequest struct {
	JoinCode    string  `json:"join_code" validate:"required,len=6"`
	FCMToken    string  `json:"fcm_token" validate:"required"`
	DisplayName *string `json:"display_name"`
	PIN         *string `json:"pin" validate:"omitempty,len=4"`
}

type JoinRequest struct {
	FCMToken    string  `json:"fcm_token" validate:"required"`
	DisplayName *string `json:"display_name"`
	PIN         *string `json:"pin" validate:"omitempty,len=4"`
}
