package mongostore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/follows"
)

type FollowRepository struct {
	collection *mongo.Collection
}

type followDocument struct {
	ID                   string     `bson:"_id,omitempty"`
	FollowerID           string     `bson:"followerId"`
	FolloweeID           string     `bson:"followeeId"`
	FollowerURI          string     `bson:"followerUri"`
	FolloweeURI          string     `bson:"followeeUri"`
	FollowerHost         *string    `bson:"followerHost"`
	FolloweeHost         *string    `bson:"followeeHost"`
	FollowerInbox        string     `bson:"followerInbox,omitempty"`
	FollowerSharedInbox  string     `bson:"followerSharedInbox,omitempty"`
	FolloweeInbox        string     `bson:"followeeInbox,omitempty"`
	FolloweeSharedInbox  string     `bson:"followeeSharedInbox,omitempty"`
	CreatedAt            time.Time  `bson:"createdAt"`
	RemoteActivityID     string     `bson:"remoteActivityId,omitempty"`
	RemoteUndoActivityID string     `bson:"remoteUndoActivityId,omitempty"`
	DeletedAt            *time.Time `bson:"deletedAt,omitempty"`
}

func NewFollowRepository(db *mongo.Database) *FollowRepository {
	return &FollowRepository{collection: db.Collection("follows")}
}

func (r *FollowRepository) Find(ctx context.Context, followerID, followeeID string) (*follows.Follow, error) {
	return r.findOne(ctx, bson.M{
		"followerId": followerID,
		"followeeId": followeeID,
		"deletedAt":  nil,
	})
}

func (r *FollowRepository) Upsert(ctx context.Context, follow follows.Follow) (*follows.Follow, error) {
	if follow.FollowerID == "" || follow.FolloweeID == "" {
		return nil, fmt.Errorf("followerId and followeeId are required")
	}
	if follow.ID == "" {
		follow.ID = followID(follow.FollowerID, follow.FolloweeID)
	}
	if follow.CreatedAt.IsZero() {
		follow.CreatedAt = time.Now().UTC()
	}
	doc := fromFollow(follow)
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"followerId": doc.FollowerID,
		"followeeId": doc.FolloweeID,
	}, bson.M{
		"$setOnInsert": doc,
		"$set": bson.M{
			"deletedAt": nil,
		},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.Find(ctx, follow.FollowerID, follow.FolloweeID)
}

func (r *FollowRepository) Delete(ctx context.Context, followerID, followeeID, remoteUndoActivityID string) error {
	now := time.Now().UTC()
	update := bson.M{
		"deletedAt": now,
	}
	if remoteUndoActivityID != "" {
		update["remoteUndoActivityId"] = remoteUndoActivityID
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"followerId": followerID,
		"followeeId": followeeID,
		"deletedAt":  nil,
	}, bson.M{"$set": update})
	return err
}

func (r *FollowRepository) CountFollowers(ctx context.Context, followeeID string) (int, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"followeeId": followeeID,
		"deletedAt":  nil,
	})
	return int(count), err
}

func (r *FollowRepository) CountFollowing(ctx context.Context, followerID string) (int, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{
		"followerId": followerID,
		"deletedAt":  nil,
	})
	return int(count), err
}

func (r *FollowRepository) ListFollowers(ctx context.Context, followeeID string, limit int) ([]follows.Follow, error) {
	return r.findMany(ctx, bson.M{
		"followeeId": followeeID,
		"deletedAt":  nil,
	}, limit)
}

func (r *FollowRepository) ListFollowing(ctx context.Context, followerID string, limit int) ([]follows.Follow, error) {
	return r.findMany(ctx, bson.M{
		"followerId": followerID,
		"deletedAt":  nil,
	}, limit)
}

func (r *FollowRepository) findOne(ctx context.Context, filter bson.M) (*follows.Follow, error) {
	var doc followDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toFollow(doc), nil
}

func (r *FollowRepository) findMany(ctx context.Context, filter bson.M, limit int) ([]follows.Follow, error) {
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	cursor, err := r.collection.Find(ctx, filter, options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []followDocument
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]follows.Follow, 0, len(docs))
	for _, doc := range docs {
		out = append(out, *toFollow(doc))
	}
	return out, nil
}

func fromFollow(follow follows.Follow) followDocument {
	return followDocument{
		ID:                   follow.ID,
		FollowerID:           follow.FollowerID,
		FolloweeID:           follow.FolloweeID,
		FollowerURI:          follow.FollowerURI,
		FolloweeURI:          follow.FolloweeURI,
		FollowerHost:         follow.FollowerHost,
		FolloweeHost:         follow.FolloweeHost,
		FollowerInbox:        follow.FollowerInbox,
		FollowerSharedInbox:  follow.FollowerSharedInbox,
		FolloweeInbox:        follow.FolloweeInbox,
		FolloweeSharedInbox:  follow.FolloweeSharedInbox,
		CreatedAt:            follow.CreatedAt,
		RemoteActivityID:     follow.RemoteActivityID,
		RemoteUndoActivityID: follow.RemoteUndoActivityID,
	}
}

func toFollow(doc followDocument) *follows.Follow {
	return &follows.Follow{
		ID:                   doc.ID,
		FollowerID:           doc.FollowerID,
		FolloweeID:           doc.FolloweeID,
		FollowerURI:          doc.FollowerURI,
		FolloweeURI:          doc.FolloweeURI,
		FollowerHost:         doc.FollowerHost,
		FolloweeHost:         doc.FolloweeHost,
		FollowerInbox:        doc.FollowerInbox,
		FollowerSharedInbox:  doc.FollowerSharedInbox,
		FolloweeInbox:        doc.FolloweeInbox,
		FolloweeSharedInbox:  doc.FolloweeSharedInbox,
		CreatedAt:            doc.CreatedAt,
		RemoteActivityID:     doc.RemoteActivityID,
		RemoteUndoActivityID: doc.RemoteUndoActivityID,
	}
}

func followID(followerID, followeeID string) string {
	sum := sha256.Sum256([]byte(followerID + "\x00" + followeeID))
	return "follow_" + hex.EncodeToString(sum[:])[:24]
}
