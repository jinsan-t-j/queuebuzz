package services

import (
	"context"
	"crypto/rand"
	"math/big"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/models"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type HOST_SERVICE struct {
	rdb     *redis.Client
	hostCol *mongo.Collection
}

func NewHostService(rdb *redis.Client, hostCol *mongo.Collection) *HOST_SERVICE {
	return &HOST_SERVICE{rdb: rdb, hostCol: hostCol}
}

func (hs *HOST_SERVICE) FindOrCreateHost(ctx context.Context, email, phone string) (*models.Host, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter := bson.M{}
	if email != "" {
		filter["email"] = email
	} else if phone != "" {
		filter["phone"] = phone
	}

	var host models.Host
	err := hs.hostCol.FindOne(ctx, filter).Decode(&host)
	if err == nil {
		// Update last_seen
		_, _ = hs.hostCol.UpdateOne(ctx, bson.M{"_id": host.ID}, bson.M{"$set": bson.M{"last_seen": time.Now()}})
		return &host, nil
	}

	// Create new host
	now := time.Now()
	host = models.Host{
		ID:        generateHostID(),
		PublicID:  generateSlug(),
		Tier:      constants.TierFree,
		CreatedAt: now,
		LastSeen:  now,
	}
	if email != "" {
		host.Email = &email
	}
	if phone != "" {
		host.Phone = &phone
	}

	_, err = hs.hostCol.InsertOne(ctx, host)
	if err != nil {
		return nil, err
	}

	return &host, nil
}

func generateHostID() string {
	return "host_" + uuid.New().String()[:8]
}

func generateSlug() string {
	adjectives := []string{"swift", "bright", "calm", "bold", "cool", "fast", "keen", "neat", "warm", "wise"}
	nouns := []string{"queue", "spot", "line", "desk", "gate", "lane", "zone", "hub", "dock", "pass"}

	adj := adjectives[randInt(len(adjectives))]
	noun := nouns[randInt(len(nouns))]
	num := 1000 + randInt(9000)

	return adj + "-" + noun + "-" + itoa(num)
}

func randInt(max int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max)))
	return int(n.Int64())
}

func itoa(n int) string {
	s := make([]byte, 4)
	for i := 3; i >= 0; i-- {
		s[i] = '0' + byte(n%10)
		n /= 10
	}
	return string(s)
}
