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

type Host struct {
	ID         string          `bson:"_id" json:"id"`
	PublicID   string          `bson:"public_id" json:"public_id"`
	Email      *string         `bson:"email,omitempty" json:"-"`
	Phone      *string         `bson:"phone,omitempty" json:"-"`
	SocialAuth *HostSocialAuth `bson:"social_auth,omitempty" json:"-"`
	Tier       string          `bson:"tier" json:"tier"`
	CreatedAt  time.Time       `bson:"created_at" json:"created_at"`
	LastSeen   time.Time       `bson:"last_seen" json:"last_seen"`
}
