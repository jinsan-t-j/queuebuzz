package models

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type QueueEntry struct {
	Token       string    `bson:"token"        json:"token"`
	QueueID     string    `bson:"queue_id"     json:"queue_id"`
	TicketNo    string    `bson:"ticket_no"    json:"ticket_no"`
	Position    int       `bson:"position"     json:"position"`
	Status      string    `bson:"status"       json:"status"`
	JoinedAt    time.Time `bson:"joined_at"    json:"joined_at"`
	FCMToken    string    `bson:"fcm_token"    json:"-"`
	Email       *string   `bson:"email"        json:"-"`
	DisplayName *string   `bson:"display_name" json:"display_name,omitempty"`
	PINHash     *string   `bson:"pin_hash"     json:"-"`
	Metadata    bson.M    `bson:"metadata"     json:"-"`
}
