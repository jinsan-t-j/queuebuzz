package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Entry struct {
	ID         string     `bson:"_id" json:"id"`
	Token      string     `bson:"token" json:"token"`
	QueueID    string     `bson:"queue_id" json:"queue_id"`
	TicketNo   string     `bson:"ticket_no" json:"ticket_number"`
	Status     string     `bson:"status" json:"status"`
	FCMToken   *string    `bson:"fcm_token" json:"-"`
	Email      *string    `bson:"email" json:"-"`
	Phone      *string    `bson:"phone" json:"-"`
	PartySize  *int       `bson:"party_size" json:"party_size,omitempty"`
	Name       string     `bson:"name" json:"name,omitempty"`
	Metadata   bson.M     `bson:"metadata" json:"-"`
	ServedAt   *time.Time `bson:"served_at" json:"served_at,omitempty"`
	FinishedAt *time.Time `bson:"finished_at" json:"finished_at,omitempty"`
	CreatedAt  time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt  time.Time  `bson:"updated_at" json:"updated_at"`
	CreatedBy  *string    `bson:"created_by" json:"created_by,omitempty"`
}
