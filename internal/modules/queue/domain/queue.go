package domain

import "time"

type Queue struct {
	ID                string    `bson:"_id" json:"id"`
	Name              string    `bson:"name" json:"name"`
	HostID            *string   `bson:"host_id" json:"-"`
	HostPublicID      *string   `bson:"host_public_id" json:"host_public_id,omitempty"`
	JoinCode          string    `bson:"join_code" json:"join_code"`
	Slug              string    `bson:"slug" json:"slug"`
	Status            string    `bson:"status" json:"status"`
	AvgServiceMins    int       `bson:"avg_service_mins" json:"avg_service_mins"`
	AllowPartyJoining bool      `bson:"allow_party_joining" json:"allow_party_joining"`
	MaxPartySize      int       `bson:"max_party_size" json:"max_party_size"`
	CreatedAt         time.Time `bson:"created_at" json:"created_at"`
	ExpiresAt         time.Time `bson:"expires_at" json:"expires_at"`
}
