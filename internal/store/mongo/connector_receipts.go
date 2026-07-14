package mongostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/nexryai/rosmarinus/internal/connector"
)

type ConnectorReceiptRepository struct {
	collection *mongo.Collection
}

type connectorReceiptDocument struct {
	ID        bson.ObjectID                  `bson:"_id,omitempty"`
	AccountID string                         `bson:"accountId"`
	ClientID  string                         `bson:"clientId"`
	RequestID string                         `bson:"requestId"`
	Command   string                         `bson:"command"`
	ActorID   string                         `bson:"actorId"`
	Status    connector.CommandReceiptStatus `bson:"status"`
	Result    any                            `bson:"result,omitempty"`
	ErrorCode string                         `bson:"errorCode,omitempty"`
	CreatedAt time.Time                      `bson:"createdAt"`
	UpdatedAt time.Time                      `bson:"updatedAt"`
	ExpiresAt time.Time                      `bson:"expiresAt"`
}

func NewConnectorReceiptRepository(db *mongo.Database) *ConnectorReceiptRepository {
	return &ConnectorReceiptRepository{collection: db.Collection("connector_command_receipts")}
}

func (r *ConnectorReceiptRepository) Claim(ctx context.Context, receipt connector.CommandReceipt) (*connector.CommandReceipt, bool, error) {
	if r == nil || r.collection == nil {
		return nil, false, fmt.Errorf("connector receipt collection is not configured")
	}
	doc := connectorReceiptDocument{
		AccountID: receipt.AccountID,
		ClientID:  receipt.ClientID,
		RequestID: receipt.RequestID,
		Command:   receipt.Command,
		ActorID:   receipt.ActorID,
		Status:    connector.CommandReceiptPending,
		CreatedAt: receipt.CreatedAt,
		UpdatedAt: receipt.UpdatedAt,
		ExpiresAt: receipt.ExpiresAt,
	}
	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		if !mongo.IsDuplicateKeyError(err) {
			return nil, false, err
		}
		existing, findErr := r.find(ctx, receipt.AccountID, receipt.RequestID)
		return existing, false, findErr
	}
	claimed := receipt
	claimed.Status = connector.CommandReceiptPending
	return &claimed, true, nil
}

func (r *ConnectorReceiptRepository) Complete(ctx context.Context, accountID, requestID string, result any, now time.Time) error {
	return r.transition(ctx, accountID, requestID, bson.M{
		"status":    connector.CommandReceiptCompleted,
		"result":    result,
		"errorCode": "",
		"updatedAt": now,
	})
}

func (r *ConnectorReceiptRepository) Fail(ctx context.Context, accountID, requestID, code string, now time.Time) error {
	return r.transition(ctx, accountID, requestID, bson.M{
		"status":    connector.CommandReceiptFailed,
		"errorCode": code,
		"updatedAt": now,
	})
}

func (r *ConnectorReceiptRepository) transition(ctx context.Context, accountID, requestID string, set bson.M) error {
	if r == nil || r.collection == nil {
		return fmt.Errorf("connector receipt collection is not configured")
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"accountId": accountID,
		"requestId": requestID,
		"status":    connector.CommandReceiptPending,
	}, bson.M{"$set": set})
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return fmt.Errorf("pending connector receipt not found")
	}
	return nil
}

func (r *ConnectorReceiptRepository) find(ctx context.Context, accountID, requestID string) (*connector.CommandReceipt, error) {
	var doc connectorReceiptDocument
	if err := r.collection.FindOne(ctx, bson.M{"accountId": accountID, "requestId": requestID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &connector.CommandReceipt{
		AccountID: doc.AccountID,
		ClientID:  doc.ClientID,
		RequestID: doc.RequestID,
		Command:   doc.Command,
		ActorID:   doc.ActorID,
		Status:    doc.Status,
		Result:    doc.Result,
		ErrorCode: doc.ErrorCode,
		CreatedAt: doc.CreatedAt,
		UpdatedAt: doc.UpdatedAt,
		ExpiresAt: doc.ExpiresAt,
	}, nil
}
