package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"queuebuzz/internal/constants"
	"queuebuzz/internal/helpers"
	authdomain "queuebuzz/internal/modules/auth/domain"
	hostdomain "queuebuzz/internal/modules/host/domain"
	"queuebuzz/internal/services"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

type Service struct {
	hostCol  *mongodriver.Collection
	queueCol *mongodriver.Collection
	emailSvc *services.EmailService
}

func New(hostCol, queueCol *mongodriver.Collection, emailSvc *services.EmailService) *Service {
	return &Service{
		hostCol:  hostCol,
		queueCol: queueCol,
		emailSvc: emailSvc,
	}
}

func (s *Service) FindOrCreateHost(ctx context.Context, email, phone string) (*hostdomain.Host, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	filter := bson.M{}
	if email != "" {
		filter["email"] = email
	} else if phone != "" {
		filter["phone"] = phone
	}

	var host hostdomain.Host
	err := s.hostCol.FindOne(ctx, filter).Decode(&host)
	if err == nil {
		_, _ = s.hostCol.UpdateOne(ctx, bson.M{"_id": host.ID}, bson.M{"$set": bson.M{"last_seen": time.Now()}})
		return &host, false, nil
	}

	now := time.Now()
	host = hostdomain.Host{
		ID:            generateHostID(),
		PublicID:      helpers.GenerateSlug(),
		Tier:          constants.TierFree,
		TermsAccepted: false,
		CreatedAt:     now,
		LastSeen:      now,
	}
	if email != "" {
		host.Email = &email
	}
	if phone != "" {
		host.Phone = &phone
	}

	if _, insertErr := s.hostCol.InsertOne(ctx, host); insertErr != nil {
		// Concurrent FindOrCreateHost for the same email/phone hit the unique index.
		// Retry the find — the other goroutine created the host successfully.
		if mongodriver.IsDuplicateKeyError(insertErr) {
			var existing hostdomain.Host
			if retryErr := s.hostCol.FindOne(ctx, filter).Decode(&existing); retryErr == nil {
				_, _ = s.hostCol.UpdateOne(ctx, bson.M{"_id": existing.ID}, bson.M{"$set": bson.M{"last_seen": time.Now()}})
				return &existing, false, nil
			}
		}
		return nil, false, insertErr
	}

	if s.emailSvc != nil && email != "" {
		_ = s.emailSvc.SendWelcomeEmail(email, "there")
	}

	return &host, true, nil
}

func (s *Service) FindOrCreateHostBySocial(ctx context.Context, identity *authdomain.SocialIdentity) (*hostdomain.Host, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	providerField := fmt.Sprintf("social_auth.%s.provider_user_id", identity.Provider)
	var host hostdomain.Host
	err := s.hostCol.FindOne(ctx, bson.M{providerField: identity.ProviderUserID}).Decode(&host)
	if err == nil {
		_, _ = s.hostCol.UpdateOne(ctx, bson.M{"_id": host.ID}, bson.M{"$set": bson.M{"last_seen": time.Now()}})
		return &host, false, nil
	}
	if err != mongodriver.ErrNoDocuments {
		return nil, false, err
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
		err = s.hostCol.FindOne(ctx, bson.M{"email": identity.Email}).Decode(&host)
		if err == nil {
			existing := providerAuthForHost(&host, identity.Provider)
			if existing != nil && existing.ProviderUserID != identity.ProviderUserID {
				return nil, false, fmt.Errorf("account already linked with a different %s identity", identity.Provider)
			}
			_, err := s.hostCol.UpdateOne(ctx, bson.M{"_id": host.ID}, bson.M{"$set": bson.M{
				"last_seen":                        now,
				"social_auth." + identity.Provider: providerAuth,
			}})
			if err != nil {
				return nil, false, err
			}
			applyProviderAuth(&host, identity.Provider, providerAuth)
			host.LastSeen = time.Now()
			return &host, false, nil
		}
		if err != mongodriver.ErrNoDocuments {
			return nil, false, err
		}
	}

	host = hostdomain.Host{
		ID:            generateHostID(),
		PublicID:      helpers.GenerateSlug(),
		Tier:          constants.TierFree,
		TermsAccepted: false,
		CreatedAt:     now,
		LastSeen:      now,
		SocialAuth:    &hostdomain.HostSocialAuth{},
	}
	if identity.EmailVerified && identity.Email != "" {
		host.Email = &identity.Email
	}
	applyProviderAuth(&host, identity.Provider, providerAuth)

	if _, insertErr := s.hostCol.InsertOne(ctx, host); insertErr != nil {
		if mongodriver.IsDuplicateKeyError(insertErr) {
			var existing hostdomain.Host
			if retryErr := s.hostCol.FindOne(ctx, bson.M{providerField: identity.ProviderUserID}).Decode(&existing); retryErr == nil {
				_, _ = s.hostCol.UpdateOne(ctx, bson.M{"_id": existing.ID}, bson.M{"$set": bson.M{"last_seen": time.Now()}})
				return &existing, false, nil
			}
		}
		return nil, false, insertErr
	}
	return &host, true, nil
}

func (s *Service) FindByID(ctx context.Context, id string) (*hostdomain.Host, error) {
	var host hostdomain.Host
	if err := s.hostCol.FindOne(ctx, bson.M{"_id": id}).Decode(&host); err != nil {
		return nil, err
	}
	return &host, nil
}

func (s *Service) FindByPublicID(ctx context.Context, publicID string) (*hostdomain.Host, error) {
	var host hostdomain.Host
	if err := s.hostCol.FindOne(ctx, bson.M{"public_id": publicID}).Decode(&host); err != nil {
		return nil, err
	}
	return &host, nil
}

func (s *Service) ClaimQueue(ctx context.Context, queueID, hostID, publicID string) error {
	_, err := s.queueCol.UpdateOne(ctx, bson.M{"_id": queueID}, bson.M{"$set": bson.M{
		"host_id":        hostID,
		"host_public_id": publicID,
	}})
	return err
}

var slugRegex = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var reservedSlugs = map[string]bool{
	"api":      true,
	"admin":    true,
	"queue":    true,
	"host":     true,
	"billing":  true,
	"pricing":  true,
	"status":   true,
	"live":     true,
	"login":    true,
	"register": true,
}

func (s *Service) UpdateHost(ctx context.Context, id string, updates bson.M) error {
	if slugVal, ok := updates["slug"]; ok {
		if slugStr, ok := slugVal.(string); ok && slugStr != "" {
			slug := strings.ToLower(strings.TrimSpace(slugStr))
			if len(slug) < 3 || len(slug) > 30 || !slugRegex.MatchString(slug) {
				return fmt.Errorf("invalid slug format: must be 3-30 lowercase alphanumeric characters or hyphens")
			}
			if reservedSlugs[slug] {
				return fmt.Errorf("this custom slug is reserved and cannot be used")
			}

			// Ensure unique slug across hosts
			count, err := s.hostCol.CountDocuments(ctx, bson.M{
				"_id":  bson.M{"$ne": id},
				"slug": slug,
			})
			if err != nil {
				return fmt.Errorf("failed to validate slug uniqueness: %w", err)
			}
			if count > 0 {
				return fmt.Errorf("this custom slug is already taken by another business")
			}

			// Ensure no collision with active queues' slugs, unless they belong to the same host
			qCount, err := s.queueCol.CountDocuments(ctx, bson.M{
				"slug":    slug,
				"status":  constants.QueueStatusActive,
				"host_id": bson.M{"$ne": id},
			})
			if err != nil {
				return fmt.Errorf("failed to validate slug uniqueness against queues: %w", err)
			}
			if qCount > 0 {
				return fmt.Errorf("this custom slug is already in use by an active queue")
			}

			// Write the normalized/sanitized slug back
			updates["slug"] = slug
		}
	}

	_, err := s.hostCol.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": updates})
	return err
}

func (s *Service) DeleteHost(ctx context.Context, id string) error {
	var host hostdomain.Host
	if err := s.hostCol.FindOne(ctx, bson.M{"_id": id}).Decode(&host); err == nil {
		if s.emailSvc != nil && host.Email != nil {
			hostName := "there"
			if host.Name != "" {
				hostName = host.Name
			}
			_ = s.emailSvc.SendAccountDeletionEmail(*host.Email, hostName)
		}
	}

	_, _ = s.queueCol.DeleteMany(ctx, bson.M{"host_id": id})
	_, err := s.hostCol.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *Service) CheckAuthMethod(ctx context.Context, email string) (method string, provider string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	var host hostdomain.Host
	err = s.hostCol.FindOne(ctx, bson.M{"email": email}).Decode(&host)
	if err != nil {
		if err == mongodriver.ErrNoDocuments {
			return "magic-link", "", nil
		}
		return "", "", err
	}

	if host.SocialAuth != nil {
		if host.SocialAuth.Google != nil {
			return "social", "google", nil
		}
		if host.SocialAuth.Apple != nil {
			return "social", "apple", nil
		}
	}

	return "magic-link", "", nil
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
