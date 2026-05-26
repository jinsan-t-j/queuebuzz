package domain

import "time"

type SocialProviderAuth struct {
	ProviderUserID string    `bson:"provider_user_id" json:"provider_user_id"`
	Email          string    `bson:"email,omitempty" json:"email,omitempty"`
	LinkedAt       time.Time `bson:"linked_at" json:"linked_at"`
	LastLoginAt    time.Time `bson:"last_login_at" json:"last_login_at"`
}

type HostSocialAuth struct {
	Google *SocialProviderAuth `bson:"google,omitempty" json:"google,omitempty"`
	Apple  *SocialProviderAuth `bson:"apple,omitempty" json:"apple,omitempty"`
}

type HostSettings struct {
	DefaultQueueName   string `bson:"default_queue_name" json:"default_queue_name"`
	AvgServiceMins     int    `bson:"avg_service_mins" json:"avg_service_mins"`
	EmailNotifications bool   `bson:"email_notifications" json:"email_notifications"`
	PushNotifications  bool   `bson:"push_notifications" json:"push_notifications"`
	CollectEmails      bool   `bson:"collect_emails" json:"collect_emails"`
}

type Host struct {
	ID                  string          `bson:"_id" json:"id"`
	PublicID            string          `bson:"public_id" json:"public_id"`
	Slug                string          `bson:"slug" json:"slug"`
	Name                string          `bson:"name" json:"name"`
	BusinessName        string          `bson:"business_name" json:"business_name"`
	Address             string          `bson:"address" json:"address"`
	Email               *string         `bson:"email,omitempty" json:"-"`
	Phone               *string         `bson:"phone,omitempty" json:"-"`
	SocialAuth          *HostSocialAuth `bson:"social_auth,omitempty" json:"-"`
	Tier                string          `bson:"tier" json:"tier"`
	MonthlyQueueCount   int             `bson:"monthly_queue_count" json:"monthly_queue_count"`
	TotalQueueCount     int             `bson:"total_queue_count" json:"total_queue_count"`
	TotalServedCount    int             `bson:"total_served_count" json:"total_served_count"`
	MonthlyLimitResetAt time.Time       `bson:"monthly_limit_reset_at" json:"monthly_limit_reset_at"`
	Settings            *HostSettings   `bson:"settings,omitempty" json:"settings,omitempty"`
	ProfileImageURL     *string         `bson:"profile_image_url,omitempty" json:"profile_image_url,omitempty"`
	BannerImageURL      *string         `bson:"banner_image_url,omitempty" json:"banner_image_url,omitempty"`
	CreatedAt           time.Time       `bson:"created_at" json:"created_at"`
	LastSeen            time.Time       `bson:"last_seen" json:"last_seen"`
}
