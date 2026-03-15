package repository

import (
	"context"
	"time"

	hostdomain "queuebuzz/internal/modules/host/domain"

	"go.mongodb.org/mongo-driver/v2/bson"
	mongodriver "go.mongodb.org/mongo-driver/v2/mongo"
)

type MongoRepository struct {
	hostCol  *mongodriver.Collection
	queueCol *mongodriver.Collection
}

func NewMongoRepository(hostCol, queueCol *mongodriver.Collection) *MongoRepository {
	return &MongoRepository{hostCol: hostCol, queueCol: queueCol}
}

func (r *MongoRepository) FindByID(ctx context.Context, id string) (*hostdomain.Host, error) {
	var host hostdomain.Host
	if err := r.hostCol.FindOne(ctx, bson.M{"_id": id}).Decode(&host); err != nil {
		return nil, err
	}
	return &host, nil
}

func (r *MongoRepository) FindByPublicID(ctx context.Context, publicID string) (*hostdomain.Host, error) {
	var host hostdomain.Host
	if err := r.hostCol.FindOne(ctx, bson.M{"public_id": publicID}).Decode(&host); err != nil {
		return nil, err
	}
	return &host, nil
}

func (r *MongoRepository) FindOneByFilter(ctx context.Context, filter bson.M) (*hostdomain.Host, error) {
	var host hostdomain.Host
	if err := r.hostCol.FindOne(ctx, filter).Decode(&host); err != nil {
		return nil, err
	}
	return &host, nil
}

func (r *MongoRepository) Insert(ctx context.Context, host hostdomain.Host) error {
	_, err := r.hostCol.InsertOne(ctx, host)
	return err
}

func (r *MongoRepository) UpdateLastSeen(ctx context.Context, id string, at time.Time) error {
	_, err := r.hostCol.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"last_seen": at}})
	return err
}

func (r *MongoRepository) UpdateSocialLink(ctx context.Context, id string, provider string, auth *hostdomain.SocialProviderAuth, at time.Time) error {
	_, err := r.hostCol.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"last_seen":               at,
		"social_auth." + provider: auth,
		"social_auth." + provider + ".last_login_at": at,
	}})
	return err
}

func (r *MongoRepository) ClaimQueue(ctx context.Context, queueID, hostID, publicID string) error {
	_, err := r.queueCol.UpdateOne(ctx, bson.M{"_id": queueID}, bson.M{"$set": bson.M{
		"host_id":        hostID,
		"host_public_id": publicID,
	}})
	return err
}
