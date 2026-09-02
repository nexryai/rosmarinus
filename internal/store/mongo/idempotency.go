package mongostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/nexryai/rosmarinus/internal/idempotency"
)

type IdempotencyRepository struct {
	collection *mongo.Collection
}

type idempotencyDocument struct {
	ID         bson.ObjectID      `bson:"_id,omitempty"`
	AccountID  string             `bson:"accountId"`
	Key        string             `bson:"key"`
	Operation  string             `bson:"operation"`
	ActorID    string             `bson:"actorId"`
	IntentHash string             `bson:"intentHash"`
	Status     idempotency.Status `bson:"status"`
	Result     any                `bson:"result,omitempty"`
	ErrorCode  string             `bson:"errorCode,omitempty"`
	CreatedAt  time.Time          `bson:"createdAt"`
	UpdatedAt  time.Time          `bson:"updatedAt"`
	ExpiresAt  time.Time          `bson:"expiresAt"`
}

func NewIdempotencyRepository(db *mongo.Database) *IdempotencyRepository {
	return &IdempotencyRepository{collection: db.Collection("api_idempotency_receipts")}
}

func (r *IdempotencyRepository) Claim(ctx context.Context, receipt idempotency.Receipt) (*idempotency.Receipt, bool, error) {
	if r == nil || r.collection == nil {
		return nil, false, fmt.Errorf("idempotency collection is not configured")
	}
	doc := idempotencyDocument{
		AccountID: receipt.AccountID, Key: receipt.Key, Operation: receipt.Operation,
		ActorID: receipt.ActorID, IntentHash: receipt.IntentHash, Status: idempotency.StatusPending,
		CreatedAt: receipt.CreatedAt, UpdatedAt: receipt.UpdatedAt, ExpiresAt: receipt.ExpiresAt,
	}
	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		if !mongo.IsDuplicateKeyError(err) {
			return nil, false, err
		}
		existing, findErr := r.find(ctx, receipt.AccountID, receipt.Key)
		return existing, false, findErr
	}
	claimed := receipt
	claimed.Status = idempotency.StatusPending
	return &claimed, true, nil
}

func (r *IdempotencyRepository) Complete(ctx context.Context, accountID, key, actorID string, result any, now time.Time) error {
	return r.transition(ctx, accountID, key, bson.M{
		"status": idempotency.StatusCompleted, "actorId": actorID, "result": result,
		"errorCode": "", "updatedAt": now,
	})
}

func (r *IdempotencyRepository) Fail(ctx context.Context, accountID, key, code string, now time.Time) error {
	return r.transition(ctx, accountID, key, bson.M{
		"status": idempotency.StatusFailed, "errorCode": code, "updatedAt": now,
	})
}

func (r *IdempotencyRepository) transition(ctx context.Context, accountID, key string, set bson.M) error {
	if r == nil || r.collection == nil {
		return fmt.Errorf("idempotency collection is not configured")
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"accountId": accountID, "key": key, "status": idempotency.StatusPending,
	}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return fmt.Errorf("pending idempotency receipt not found")
	}
	return nil
}

func (r *IdempotencyRepository) find(ctx context.Context, accountID, key string) (*idempotency.Receipt, error) {
	var doc idempotencyDocument
	if err := r.collection.FindOne(ctx, bson.M{"accountId": accountID, "key": key}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &idempotency.Receipt{
		AccountID: doc.AccountID, Key: doc.Key, Operation: doc.Operation, ActorID: doc.ActorID, IntentHash: doc.IntentHash,
		Status: doc.Status, Result: doc.Result, ErrorCode: doc.ErrorCode,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, ExpiresAt: doc.ExpiresAt,
	}, nil
}

var _ idempotency.Store = (*IdempotencyRepository)(nil)
