package mongostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/mutes"
)

type MuteRepository struct {
	collection *mongo.Collection
}

type muteDocument struct {
	ID        string     `bson:"_id,omitempty"`
	MuterID   string     `bson:"muterId"`
	MuteeID   string     `bson:"muteeId"`
	CreatedAt time.Time  `bson:"createdAt"`
	ExpiresAt *time.Time `bson:"expiresAt,omitempty"`
}

func NewMuteRepository(db *mongo.Database) *MuteRepository {
	return &MuteRepository{collection: db.Collection("mutes")}
}

func activeMuteFilter(muterID string, now time.Time) bson.M {
	return bson.M{
		"muterId": muterID,
		"$or": bson.A{
			bson.M{"expiresAt": nil},
			bson.M{"expiresAt": bson.M{"$gt": now}},
		},
	}
}

func (r *MuteRepository) FindActive(ctx context.Context, muterID, muteeID string, now time.Time) (*mutes.Mute, error) {
	filter := activeMuteFilter(muterID, now)
	filter["muteeId"] = muteeID
	var doc muteDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toMute(doc), nil
}

func (r *MuteRepository) ListActiveMuteeIDs(ctx context.Context, muterID string, now time.Time) ([]string, error) {
	cursor, err := r.collection.Find(ctx, activeMuteFilter(muterID, now), options.Find().SetProjection(bson.M{"muteeId": 1}).SetSort(bson.D{{Key: "muteeId", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []muteDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(docs))
	for _, doc := range docs {
		if doc.MuteeID != "" {
			result = append(result, doc.MuteeID)
		}
	}
	return result, nil
}

func (r *MuteRepository) Upsert(ctx context.Context, mute mutes.Mute) (*mutes.Mute, error) {
	if mute.MuterID == "" || mute.MuteeID == "" {
		return nil, fmt.Errorf("muterId and muteeId are required")
	}
	id, err := newDocumentID(ctx, r.collection)
	if err != nil {
		return nil, fmt.Errorf("generate mute id: %w", err)
	}
	if mute.CreatedAt.IsZero() {
		mute.CreatedAt = time.Now().UTC()
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"muterId": mute.MuterID, "muteeId": mute.MuteeID}, bson.M{
		"$setOnInsert": bson.M{"_id": id, "muterId": mute.MuterID, "muteeId": mute.MuteeID, "createdAt": mute.CreatedAt},
		"$set":         bson.M{"expiresAt": mute.ExpiresAt},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindActive(ctx, mute.MuterID, mute.MuteeID, mute.CreatedAt)
}

func (r *MuteRepository) Delete(ctx context.Context, muterID, muteeID string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"muterId": muterID, "muteeId": muteeID})
	return err
}

func toMute(doc muteDocument) *mutes.Mute {
	return &mutes.Mute{ID: doc.ID, MuterID: doc.MuterID, MuteeID: doc.MuteeID, CreatedAt: doc.CreatedAt, ExpiresAt: doc.ExpiresAt}
}
