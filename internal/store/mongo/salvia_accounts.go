package mongostore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/nexryai/rosmarinus/internal/account"
)

const DefaultSalviaAccountCollection = "salvia_accounts"

type SalviaAccountRepository struct {
	collection *mongo.Collection
}

type salviaAccountDocument struct {
	ID            string         `bson:"_id"`
	AblyClientID  string         `bson:"ablyClientId"`
	Status        account.Status `bson:"status"`
	AuthzRevision int64          `bson:"authzRevision"`
	DeletedAt     *time.Time     `bson:"deletedAt,omitempty"`
}

func NewSalviaAccountRepository(db *mongo.Database, collectionName string) *SalviaAccountRepository {
	if strings.TrimSpace(collectionName) == "" {
		collectionName = DefaultSalviaAccountCollection
	}
	return &SalviaAccountRepository{collection: db.Collection(collectionName)}
}

func (r *SalviaAccountRepository) FindActiveByAblyClientID(ctx context.Context, clientID string) (*account.Account, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return nil, nil
	}
	return r.findOne(ctx, bson.M{
		"ablyClientId": clientID,
		"status":       account.StatusActive,
		"deletedAt":    nil,
	})
}

func (r *SalviaAccountRepository) FindByID(ctx context.Context, id string) (*account.Account, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	return r.findOne(ctx, bson.M{"_id": id})
}

func (r *SalviaAccountRepository) findOne(ctx context.Context, filter bson.M) (*account.Account, error) {
	if r == nil || r.collection == nil {
		return nil, fmt.Errorf("salvia account collection is not configured")
	}
	var doc salviaAccountDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &account.Account{
		ID:            doc.ID,
		AblyClientID:  doc.AblyClientID,
		Status:        doc.Status,
		AuthzRevision: doc.AuthzRevision,
		DeletedAt:     doc.DeletedAt,
	}, nil
}
