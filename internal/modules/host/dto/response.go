package dto

type HostSettingsResponse struct {
	DefaultQueueName   string `json:"default_queue_name"`
	AvgServiceMins     int    `json:"avg_service_mins"`
	EmailNotifications bool   `json:"email_notifications"`
	PushNotifications  bool   `json:"push_notifications"`
}

type GetMeResponse struct {
	ID              string                `json:"id"`
	PublicID        string                `json:"public_id"`
	Slug            string                `json:"slug"`
	Name            string                `json:"name"`
	BusinessName    string                `json:"business_name"`
	Address         string                `json:"address"`
	Email           string                `json:"email"`
	Phone           string                `json:"phone"`
	Tier            string                `json:"tier"`
	Avatar          string                `json:"avatar,omitempty"`
	ProfileImageURL string                `json:"profile_image_url,omitempty"`
	BannerImageURL  string                `json:"banner_image_url,omitempty"`
	Settings        *HostSettingsResponse `json:"settings,omitempty"`
	TermsAccepted   bool                  `json:"terms_accepted"`
}
