package mongostore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	appauth "github.com/nexryai/rosmarinus/internal/auth"
)

type WebAuthnCeremonyRepository struct {
	collection *mongo.Collection
}

type webAuthnCeremonyDocument struct {
	ID        string               `bson:"_id"`
	Type      appauth.CeremonyType `bson:"type"`
	AccountID string               `bson:"accountId,omitempty"`
	Session   []byte               `bson:"session"`
	CreatedAt time.Time            `bson:"createdAt"`
	ExpiresAt time.Time            `bson:"expiresAt"`
}

func NewWebAuthnCeremonyRepository(db *mongo.Database) *WebAuthnCeremonyRepository {
	return &WebAuthnCeremonyRepository{collection: db.Collection("webauthn_challenges")}
}

func (r *WebAuthnCeremonyRepository) Create(ctx context.Context, ceremony appauth.Ceremony) error {
	if r == nil || r.collection == nil {
		return fmt.Errorf("WebAuthn ceremony collection is not configured")
	}
	session, err := json.Marshal(ceremony.Session)
	if err != nil {
		return fmt.Errorf("encode WebAuthn ceremony: %w", err)
	}
	_, err = r.collection.InsertOne(ctx, webAuthnCeremonyDocument{
		ID: ceremony.ID, Type: ceremony.Type, AccountID: ceremony.AccountID,
		Session: session, CreatedAt: ceremony.CreatedAt, ExpiresAt: ceremony.ExpiresAt,
	})
	return err
}

func (r *WebAuthnCeremonyRepository) Consume(ctx context.Context, id string, ceremonyType appauth.CeremonyType, now time.Time) (*appauth.Ceremony, error) {
	if r == nil || r.collection == nil {
		return nil, fmt.Errorf("WebAuthn ceremony collection is not configured")
	}
	var document webAuthnCeremonyDocument
	err := r.collection.FindOneAndDelete(ctx, bson.M{
		"_id": id, "type": ceremonyType, "expiresAt": bson.M{"$gt": now},
	}, options.FindOneAndDelete()).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(document.Session, &session); err != nil {
		return nil, fmt.Errorf("decode WebAuthn ceremony: %w", err)
	}
	return &appauth.Ceremony{
		ID: document.ID, Type: document.Type, AccountID: document.AccountID,
		Session: session, CreatedAt: document.CreatedAt, ExpiresAt: document.ExpiresAt,
	}, nil
}
