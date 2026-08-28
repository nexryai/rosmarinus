package mongostore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/activities"
)

const (
	activityReceiptProcessing = "processing"
	activityReceiptCompleted  = "completed"
)

type ActivityReceiptRepository struct {
	collection *mongo.Collection
}

type activityReceiptDocument struct {
	ActivityID     string     `bson:"_id"`
	ActorURI       string     `bson:"actorUri"`
	Status         string     `bson:"status"`
	LeaseToken     string     `bson:"leaseToken,omitempty"`
	LeaseExpiresAt *time.Time `bson:"leaseExpiresAt,omitempty"`
	CreatedAt      time.Time  `bson:"createdAt"`
	CompletedAt    *time.Time `bson:"completedAt,omitempty"`
	ExpiresAt      time.Time  `bson:"expiresAt"`
}

func NewActivityReceiptRepository(db *mongo.Database) *ActivityReceiptRepository {
	return &ActivityReceiptRepository{collection: db.Collection("inbox_activity_receipts")}
}

func (r *ActivityReceiptRepository) Claim(ctx context.Context, activityID, actorURI string, now time.Time, lease, retention time.Duration) (*activities.Claim, bool, error) {
	activityID = strings.TrimSpace(activityID)
	actorURI = strings.TrimSpace(actorURI)
	if r == nil || r.collection == nil {
		return nil, false, fmt.Errorf("activity receipt collection is not configured")
	}
	if activityID == "" || actorURI == "" {
		return nil, false, fmt.Errorf("activity id and actor uri are required")
	}
	if lease <= 0 || retention <= lease {
		return nil, false, fmt.Errorf("activity receipt retention must exceed its positive lease")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	token, err := newActivityLeaseToken()
	if err != nil {
		return nil, false, err
	}
	leaseExpiresAt := now.Add(lease)
	expiresAt := now.Add(retention)
	filter := bson.M{
		"_id":      activityID,
		"actorUri": actorURI,
		"$or": bson.A{
			bson.M{"status": bson.M{"$exists": false}},
			bson.M{"status": activityReceiptProcessing, "leaseExpiresAt": bson.M{"$lte": now}},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"status": activityReceiptProcessing, "leaseToken": token,
			"leaseExpiresAt": leaseExpiresAt, "expiresAt": expiresAt,
		},
		"$setOnInsert": bson.M{"actorUri": actorURI, "createdAt": now},
		"$unset":       bson.M{"completedAt": ""},
	}
	var doc activityReceiptDocument
	err = r.collection.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)).Decode(&doc)
	if err == nil {
		return &activities.Claim{ActivityID: activityID, ActorURI: actorURI, Token: token}, true, nil
	}
	if !mongo.IsDuplicateKeyError(err) && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, err
	}
	existing, findErr := r.find(ctx, activityID)
	if findErr != nil {
		return nil, false, findErr
	}
	if existing == nil {
		return nil, false, err
	}
	if existing.ActorURI != actorURI {
		return nil, false, fmt.Errorf("activity id is already owned by a different actor")
	}
	return nil, false, nil
}

func (r *ActivityReceiptRepository) Complete(ctx context.Context, claim activities.Claim, now time.Time) error {
	if r == nil || r.collection == nil {
		return fmt.Errorf("activity receipt collection is not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"_id": claim.ActivityID, "actorUri": claim.ActorURI,
		"status": activityReceiptProcessing, "leaseToken": claim.Token,
	}, bson.M{
		"$set":   bson.M{"status": activityReceiptCompleted, "completedAt": now.UTC()},
		"$unset": bson.M{"leaseToken": "", "leaseExpiresAt": ""},
	})
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return fmt.Errorf("active activity receipt lease not found")
	}
	return nil
}

func (r *ActivityReceiptRepository) Release(ctx context.Context, claim activities.Claim) error {
	if r == nil || r.collection == nil {
		return fmt.Errorf("activity receipt collection is not configured")
	}
	_, err := r.collection.DeleteOne(ctx, bson.M{
		"_id": claim.ActivityID, "actorUri": claim.ActorURI,
		"status": activityReceiptProcessing, "leaseToken": claim.Token,
	})
	return err
}

func (r *ActivityReceiptRepository) find(ctx context.Context, activityID string) (*activityReceiptDocument, error) {
	var doc activityReceiptDocument
	if err := r.collection.FindOne(ctx, bson.M{"_id": activityID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func newActivityLeaseToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate activity receipt lease: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
