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
	StrictQueueMode   bool      `bson:"strict_queue_mode" json:"strict_queue_mode"`
	RecoveryEmail     *string   `bson:"recovery_email" json:"recovery_email,omitempty"`
	HostFCMToken      *string   `bson:"host_fcm_token" json:"-"`
	HostFCMUpdatedAt  time.Time `bson:"host_fcm_updated_at" json:"-"`
	CreatedAt         time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt         time.Time `bson:"updated_at" json:"updated_at"`
	ExpiresAt         time.Time `bson:"expires_at" json:"expires_at"`
}

type HostHistorySummary struct {
	TotalSessions int   `bson:"total_sessions" json:"total_sessions"`
	TotalServed   int   `bson:"total_served" json:"total_served"`
	TotalWaitMS   int64 `bson:"total_wait_ms" json:"total_wait_ms"`
}
