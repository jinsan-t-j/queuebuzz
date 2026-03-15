package services

import (
	"context"
	"crypto/rand"
	"fmt"
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
	} else if phone != "" && len(phone) > 0 {
		filter["phone"] = phone
	}

	var host models.Host
	err := hs.hostCol.FindOne(ctx, filter).Decode(&host)
	if err == nil {
		_, _ = hs.hostCol.UpdateOne(ctx, bson.M{"_id": host.ID}, bson.M{"$set": bson.M{"last_seen": time.Now()}})
		return &host, nil
	}

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
	if phone != "" && len(phone) > 0 {
		host.Phone = &phone
	}

	_, err = hs.hostCol.InsertOne(ctx, host)
	if err != nil {
		return nil, err
	}

	return &host, nil
}

func (hs *HOST_SERVICE) FindOrCreateHostBySocial(ctx context.Context, identity *SocialIdentity) (*models.Host, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	providerField := fmt.Sprintf("social_auth.%s.provider_user_id", identity.Provider)
	lastLoginField := fmt.Sprintf("social_auth.%s.last_login_at", identity.Provider)

	var host models.Host
	err := hs.hostCol.FindOne(ctx, bson.M{providerField: identity.ProviderUserID}).Decode(&host)
	if err == nil {
		_, _ = hs.hostCol.UpdateOne(ctx, bson.M{"_id": host.ID}, bson.M{"$set": bson.M{
			"last_seen":    time.Now(),
			lastLoginField: time.Now(),
		}})
		return &host, nil
	}
	if err != nil && err != mongo.ErrNoDocuments {
		return nil, err
	}

	now := time.Now()
	providerAuth := &models.SocialProviderAuth{
		ProviderUserID: identity.ProviderUserID,
		LinkedAt:       now,
		LastLoginAt:    now,
	}
	if identity.EmailVerified {
		providerAuth.Email = identity.Email
	}

	if identity.EmailVerified && identity.Email != "" {
		err = hs.hostCol.FindOne(ctx, bson.M{"email": identity.Email}).Decode(&host)
		if err == nil {
			existing := providerAuthForHost(&host, identity.Provider)
			if existing != nil && existing.ProviderUserID != identity.ProviderUserID {
				return nil, fmt.Errorf("account already linked with a different %s identity", identity.Provider)
			}

			update := bson.M{"$set": bson.M{
				"last_seen": time.Now(),
				fmt.Sprintf("social_auth.%s", identity.Provider): providerAuth,
			}}
			if _, err := hs.hostCol.UpdateOne(ctx, bson.M{"_id": host.ID}, update); err != nil {
				return nil, err
			}
			applyProviderAuth(&host, identity.Provider, providerAuth)
			host.LastSeen = time.Now()
			return &host, nil
		}
		if err != nil && err != mongo.ErrNoDocuments {
			return nil, err
		}
	}

	host = models.Host{
		ID:         generateHostID(),
		PublicID:   generateSlug(),
		Tier:       constants.TierFree,
		CreatedAt:  now,
		LastSeen:   now,
		SocialAuth: &models.HostSocialAuth{},
	}
	if identity.EmailVerified && identity.Email != "" {
		host.Email = &identity.Email
	}
	applyProviderAuth(&host, identity.Provider, providerAuth)

	if _, err := hs.hostCol.InsertOne(ctx, host); err != nil {
		return nil, err
	}

	return &host, nil
}

func providerAuthForHost(host *models.Host, provider string) *models.SocialProviderAuth {
	if host.SocialAuth == nil {
		return nil
	}
	switch provider {
	case "google":
		return host.SocialAuth.Google
	case "apple":
		return host.SocialAuth.Apple
	default:
		return nil
	}
}

func applyProviderAuth(host *models.Host, provider string, auth *models.SocialProviderAuth) {
	if host.SocialAuth == nil {
		host.SocialAuth = &models.HostSocialAuth{}
	}
	switch provider {
	case "google":
		host.SocialAuth.Google = auth
	case "apple":
		host.SocialAuth.Apple = auth
	}
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
