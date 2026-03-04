package models

import (
	"time"
)

type Host struct {
	ID        string    `bson:"_id"        json:"id"`
	PublicID  string    `bson:"public_id"  json:"public_id"`
	Email     *string   `bson:"email"      json:"-"`
	Phone     *string   `bson:"phone"      json:"-"`
	Tier      string    `bson:"tier"       json:"tier"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	LastSeen  time.Time `bson:"last_seen"  json:"last_seen"`
}
