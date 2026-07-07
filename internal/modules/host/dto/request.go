package dto

type RegisterRequest struct {
	Email *string `json:"email" validate:"omitempty,email"`
	Phone *string `json:"phone" validate:"omitempty,e164"`
}

type HostSettingsRequest struct {
	DefaultQueueName   *string `json:"default_queue_name,omitempty" validate:"omitempty,max=50"`
	AvgServiceMins     *int    `json:"avg_service_mins,omitempty" validate:"omitempty,min=1,max=60"`
	EmailNotifications *bool   `json:"email_notifications,omitempty"`
	PushNotifications  *bool   `json:"push_notifications,omitempty"`
}

type UpdateMeRequest struct {
	Name            *string              `json:"name,omitempty" validate:"omitempty,max=50"`
	BusinessName    *string              `json:"business_name,omitempty" validate:"omitempty,max=100"`
	Address         *string              `json:"address,omitempty" validate:"omitempty,max=200"`
	Phone           *string              `json:"phone,omitempty" validate:"omitempty,e164"`
	ProfileImageURL *string              `json:"profile_image_url,omitempty"`
	BannerImageURL  *string              `json:"banner_image_url,omitempty"`
	Slug            *string              `json:"slug,omitempty" validate:"omitempty,slug"`
	Settings        *HostSettingsRequest `json:"settings,omitempty" validate:"omitempty"`
	TermsAccepted   *bool                `json:"terms_accepted,omitempty"`
}
