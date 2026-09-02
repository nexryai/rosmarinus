package mongostore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	appauth "github.com/nexryai/rosmarinus/internal/auth"
)

type SessionRepository struct {
	collection *mongo.Collection
}

type sessionDocument struct {
	ID        string     `bson:"_id"`
	AccountID string     `bson:"accountId"`
	TokenHash string     `bson:"tokenHash"`
	CSRFToken string     `bson:"csrfToken"`
	CreatedAt time.Time  `bson:"createdAt"`
	ExpiresAt time.Time  `bson:"expiresAt"`
	RevokedAt *time.Time `bson:"revokedAt,omitempty"`
}

func NewSessionRepository(db *mongo.Database) *SessionRepository {
	return &SessionRepository{collection: db.Collection("sessions")}
}

func (r *SessionRepository) Create(ctx context.Context, session appauth.Session) error {
	if r == nil || r.collection == nil {
		return fmt.Errorf("session collection is not configured")
	}
	_, err := r.collection.InsertOne(ctx, sessionDocument{
		ID: session.ID, AccountID: session.AccountID, TokenHash: session.TokenHash,
		CSRFToken: session.CSRFToken, CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt,
	})
	return err
}

func (r *SessionRepository) FindActiveByTokenHash(ctx context.Context, tokenHash string, now time.Time) (*appauth.Session, error) {
	if r == nil || r.collection == nil {
		return nil, fmt.Errorf("session collection is not configured")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return nil, nil
	}
	var document sessionDocument
	err := r.collection.FindOne(ctx, bson.M{
		"tokenHash": tokenHash,
		"expiresAt": bson.M{"$gt": now},
		"revokedAt": nil,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &appauth.Session{
		ID: document.ID, AccountID: document.AccountID, TokenHash: document.TokenHash,
		CSRFToken: document.CSRFToken, CreatedAt: document.CreatedAt,
		ExpiresAt: document.ExpiresAt, RevokedAt: document.RevokedAt,
	}, nil
}

func (r *SessionRepository) RevokeByTokenHash(ctx context.Context, tokenHash string, now time.Time) error {
	if r == nil || r.collection == nil {
		return fmt.Errorf("session collection is not configured")
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"tokenHash": tokenHash,
		"revokedAt": nil,
	}, bson.M{"$set": bson.M{"revokedAt": now}})
	return err
}
