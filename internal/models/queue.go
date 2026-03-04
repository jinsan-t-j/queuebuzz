package models

import (
	"time"
)

type Queue struct {
	ID             string    `bson:"_id"              json:"id"`
	HostID         *string   `bson:"host_id"          json:"-"`
	HostPublicID   *string   `bson:"host_public_id"   json:"host_public_id,omitempty"`
	HostLat        float64   `bson:"host_lat"         json:"-"`
	HostLng        float64   `bson:"host_lng"         json:"-"`
	RadiusM        int       `bson:"radius_m"         json:"radius_m"`
	JoinCode       string    `bson:"join_code"        json:"join_code"`
	Status         string    `bson:"status"           json:"status"`
	CreatedAt      time.Time `bson:"created_at"       json:"created_at"`
	ExpiresAt      time.Time `bson:"expires_at"       json:"expires_at"`
	AvgServiceMins int       `bson:"avg_service_mins" json:"avg_service_mins"`
}
