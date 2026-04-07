package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"queuebuzz/internal/constants"
	authdomain "queuebuzz/internal/modules/auth/domain"
	hostdomain "queuebuzz/internal/modules/host/domain"
	"queuebuzz/internal/modules/host/repository"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

type Service struct {
	repo *repository.MongoRepository
}

func New(repo *repository.MongoRepository) *Service {
	return &Service{repo: repo}
}

func (s *Service) FindOrCreateHost(ctx context.Context, email, phone string) (*hostdomain.Host, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter := bson.M{}
	if email != "" {
		filter["email"] = email
	} else if phone != "" {
		filter["phone"] = phone
	}

	host, err := s.repo.FindOneByFilter(ctx, filter)
	if err == nil {
		_ = s.repo.UpdateLastSeen(ctx, host.ID, time.Now())
		return host, nil
	}

	now := time.Now()
	host = &hostdomain.Host{
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

	if err := s.repo.Insert(ctx, *host); err != nil {
		return nil, err
	}
	return host, nil
}

func (s *Service) FindOrCreateHostBySocial(ctx context.Context, identity *authdomain.SocialIdentity) (*hostdomain.Host, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	providerField := fmt.Sprintf("social_auth.%s.provider_user_id", identity.Provider)
	host, err := s.repo.FindOneByFilter(ctx, bson.M{providerField: identity.ProviderUserID})
	if err == nil {
		_ = s.repo.UpdateLastSeen(ctx, host.ID, time.Now())
		return host, nil
	}
	if err != mongodriver.ErrNoDocuments {
		return nil, err
	}

	now := time.Now()
	providerAuth := &hostdomain.SocialProviderAuth{
		ProviderUserID: identity.ProviderUserID,
		LinkedAt:       now,
		LastLoginAt:    now,
	}
	if identity.EmailVerified {
		providerAuth.Email = identity.Email
	}

	if identity.EmailVerified && identity.Email != "" {
		host, err = s.repo.FindOneByFilter(ctx, bson.M{"email": identity.Email})
		if err == nil {
			existing := providerAuthForHost(host, identity.Provider)
			if existing != nil && existing.ProviderUserID != identity.ProviderUserID {
				return nil, fmt.Errorf("account already linked with a different %s identity", identity.Provider)
			}
			if err := s.repo.UpdateSocialLink(ctx, host.ID, identity.Provider, providerAuth, time.Now()); err != nil {
				return nil, err
			}
			applyProviderAuth(host, identity.Provider, providerAuth)
			host.LastSeen = time.Now()
			return host, nil
		}
		if err != mongodriver.ErrNoDocuments {
			return nil, err
		}
	}

	host = &hostdomain.Host{
		ID:         generateHostID(),
		PublicID:   generateSlug(),
		Tier:       constants.TierFree,
		CreatedAt:  now,
		LastSeen:   now,
		SocialAuth: &hostdomain.HostSocialAuth{},
	}
	if identity.EmailVerified && identity.Email != "" {
		host.Email = &identity.Email
	}
	applyProviderAuth(host, identity.Provider, providerAuth)

	if err := s.repo.Insert(ctx, *host); err != nil {
		return nil, err
	}
	return host, nil
}

func (s *Service) FindByID(ctx context.Context, id string) (*hostdomain.Host, error) {
	return s.repo.FindByID(ctx, id)
}

func (s *Service) FindByPublicID(ctx context.Context, publicID string) (*hostdomain.Host, error) {
	return s.repo.FindByPublicID(ctx, publicID)
}

func (s *Service) ClaimQueue(ctx context.Context, queueID, hostID, publicID string) error {
	return s.repo.ClaimQueue(ctx, queueID, hostID, publicID)
}

func providerAuthForHost(host *hostdomain.Host, provider string) *hostdomain.SocialProviderAuth {
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

func applyProviderAuth(host *hostdomain.Host, provider string, auth *hostdomain.SocialProviderAuth) {
	if host.SocialAuth == nil {
		host.SocialAuth = &hostdomain.HostSocialAuth{}
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

func randInt(limit int) int {
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(limit)))
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
