package dto

type CreateQueueRequest struct {
	Lat            float64 `json:"lat" validate:"required"`
	Lng            float64 `json:"lng" validate:"required"`
	RadiusM        int     `json:"radius_m"`
	AvgServiceMins int     `json:"avg_service_mins"`
}

type JoinByCodeRequest struct {
	JoinCode    string  `json:"join_code" validate:"required,len=6"`
	Lat         float64 `json:"lat" validate:"required"`
	Lng         float64 `json:"lng" validate:"required"`
	FCMToken    string  `json:"fcm_token" validate:"required"`
	DisplayName *string `json:"display_name"`
	PIN         *string `json:"pin" validate:"omitempty,len=4"`
}

type JoinRequest struct {
	Lat         float64 `json:"lat" validate:"required"`
	Lng         float64 `json:"lng" validate:"required"`
	FCMToken    string  `json:"fcm_token" validate:"required"`
	DisplayName *string `json:"display_name"`
	PIN         *string `json:"pin" validate:"omitempty,len=4"`
}
