package mongostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/antennas"
)

type AntennaRepository struct {
	collection *mongo.Collection
}

type antennaDocument struct {
	ID              string          `bson:"_id"`
	OwnerAccountID  string          `bson:"ownerAccountId"`
	OwnerActorID    string          `bson:"ownerActorId"`
	Name            string          `bson:"name"`
	Source          antennas.Source `bson:"source"`
	Users           []string        `bson:"users"`
	Keywords        [][]string      `bson:"keywords"`
	ExcludeKeywords [][]string      `bson:"excludeKeywords"`
	CaseSensitive   bool            `bson:"caseSensitive"`
	LocalOnly       bool            `bson:"localOnly"`
	ExcludeBots     bool            `bson:"excludeBots"`
	WithReplies     bool            `bson:"withReplies"`
	WithFile        bool            `bson:"withFile"`
	CreatedAt       time.Time       `bson:"createdAt"`
	UpdatedAt       time.Time       `bson:"updatedAt"`
}

func NewAntennaRepository(db *mongo.Database) *AntennaRepository {
	return &AntennaRepository{collection: db.Collection("antennas")}
}

func (r *AntennaRepository) Count(ctx context.Context, accountID, actorID string) (int64, error) {
	return r.collection.CountDocuments(ctx, antennaOwnerFilter(accountID, actorID))
}

func (r *AntennaRepository) Create(ctx context.Context, antenna antennas.Antenna) (*antennas.Antenna, error) {
	var err error
	antenna, err = antennas.Normalize(antenna)
	if err != nil {
		return nil, err
	}
	antenna.ID, err = newDocumentID(ctx, r.collection)
	if err != nil {
		return nil, fmt.Errorf("generate antenna id: %w", err)
	}
	now := time.Now().UTC()
	antenna.CreatedAt, antenna.UpdatedAt = now, now
	if _, err := r.collection.InsertOne(ctx, fromAntenna(antenna)); err != nil {
		return nil, err
	}
	return &antenna, nil
}

func (r *AntennaRepository) Update(ctx context.Context, antenna antennas.Antenna) (*antennas.Antenna, error) {
	var err error
	antenna, err = antennas.Normalize(antenna)
	if err != nil {
		return nil, err
	}
	if antenna.ID == "" {
		return nil, fmt.Errorf("antenna id is required")
	}
	antenna.UpdatedAt = time.Now().UTC()
	update := bson.M{"$set": bson.M{
		"name": antenna.Name, "source": antenna.Source, "users": antenna.Users,
		"keywords": antenna.Keywords, "excludeKeywords": antenna.ExcludeKeywords,
		"caseSensitive": antenna.CaseSensitive, "localOnly": antenna.LocalOnly,
		"excludeBots": antenna.ExcludeBots, "withReplies": antenna.WithReplies,
		"withFile": antenna.WithFile, "updatedAt": antenna.UpdatedAt,
	}}
	filter := antennaOwnerFilter(antenna.OwnerAccountID, antenna.OwnerActorID)
	filter["_id"] = antenna.ID
	var doc antennaDocument
	err = r.collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	result := toAntenna(doc)
	return &result, nil
}

func (r *AntennaRepository) Delete(ctx context.Context, accountID, actorID, antennaID string) (bool, error) {
	filter := antennaOwnerFilter(accountID, actorID)
	filter["_id"] = antennaID
	result, err := r.collection.DeleteOne(ctx, filter)
	return err == nil && result.DeletedCount == 1, err
}

func antennaOwnerFilter(accountID, actorID string) bson.M {
	return bson.M{"ownerAccountId": accountID, "ownerActorId": actorID}
}

func fromAntenna(antenna antennas.Antenna) antennaDocument {
	return antennaDocument{
		ID: antenna.ID, OwnerAccountID: antenna.OwnerAccountID, OwnerActorID: antenna.OwnerActorID,
		Name: antenna.Name, Source: antenna.Source, Users: antenna.Users,
		Keywords: antenna.Keywords, ExcludeKeywords: antenna.ExcludeKeywords,
		CaseSensitive: antenna.CaseSensitive, LocalOnly: antenna.LocalOnly,
		ExcludeBots: antenna.ExcludeBots, WithReplies: antenna.WithReplies,
		WithFile: antenna.WithFile, CreatedAt: antenna.CreatedAt, UpdatedAt: antenna.UpdatedAt,
	}
}

func toAntenna(doc antennaDocument) antennas.Antenna {
	return antennas.Antenna{
		ID: doc.ID, OwnerAccountID: doc.OwnerAccountID, OwnerActorID: doc.OwnerActorID,
		Name: doc.Name, Source: doc.Source, Users: doc.Users,
		Keywords: doc.Keywords, ExcludeKeywords: doc.ExcludeKeywords,
		CaseSensitive: doc.CaseSensitive, LocalOnly: doc.LocalOnly,
		ExcludeBots: doc.ExcludeBots, WithReplies: doc.WithReplies,
		WithFile: doc.WithFile, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
	}
}
