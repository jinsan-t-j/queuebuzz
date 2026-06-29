package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

type Entry struct {
	ID                  string     `bson:"_id" json:"id"`
	Token               string     `bson:"token" json:"token"`
	QueueID             string     `bson:"queue_id" json:"queue_id"`
	TicketNo            string     `bson:"ticket_no" json:"ticket_number"`
	VerifyCode          string     `bson:"verify_code" json:"verify_code"`
	Status              string     `bson:"status" json:"status"`
	FCMToken            *string    `bson:"fcm_token" json:"-"`
	Email               *string    `bson:"email" json:"-"`
	Phone               *string    `bson:"phone" json:"-"`
	PartySize           *int       `bson:"party_size" json:"party_size,omitempty"`
	Name                string     `bson:"name" json:"name,omitempty"`
	IdentityHash        string     `bson:"identity_hash" json:"identity_hash"`
	IsReturning         bool       `bson:"is_returning" json:"is_returning"`
	Fingerprint         string     `bson:"fingerprint" json:"fingerprint"`
	Metadata            bson.M     `bson:"metadata" json:"-"`
	ServedAt            *time.Time `bson:"served_at" json:"served_at,omitempty"`
	FinishedAt          *time.Time `bson:"finished_at" json:"finished_at,omitempty"`
	CreatedAt           time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt           time.Time  `bson:"updated_at" json:"updated_at"`
	CreatedBy           *string    `bson:"created_by" json:"created_by,omitempty"`
	RecoveryEmailSentAt *time.Time `bson:"recovery_email_sent_at,omitempty" json:"-"`
}
