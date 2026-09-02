package mongostore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/account"
	appauth "github.com/nexryai/rosmarinus/internal/auth"
)

type AccountRepository struct {
	collection *mongo.Collection
}

type accountDocument struct {
	ID            string                      `bson:"_id"`
	Username      string                      `bson:"username"`
	UsernameLower string                      `bson:"usernameLower"`
	DisplayName   string                      `bson:"displayName"`
	WebAuthnID    []byte                      `bson:"webAuthnId"`
	Status        account.Status              `bson:"status"`
	BootstrapSlot string                      `bson:"bootstrapSlot,omitempty"`
	Credentials   []passkeyCredentialDocument `bson:"credentials,omitempty"`
	CreatedAt     time.Time                   `bson:"createdAt"`
	UpdatedAt     time.Time                   `bson:"updatedAt"`
	ExpiresAt     *time.Time                  `bson:"expiresAt,omitempty"`
	DeletedAt     *time.Time                  `bson:"deletedAt,omitempty"`
}

type passkeyCredentialDocument struct {
	ID   string `bson:"id"`
	Data []byte `bson:"data"`
}

func NewAccountRepository(db *mongo.Database) *AccountRepository {
	return &AccountRepository{collection: db.Collection("accounts")}
}

func (r *AccountRepository) ReserveInitial(ctx context.Context, value account.Account, expiresAt time.Time) (*appauth.User, error) {
	if r == nil || r.collection == nil {
		return nil, fmt.Errorf("account collection is not configured")
	}
	if _, err := r.collection.DeleteMany(ctx, bson.M{
		"bootstrapSlot": "initial", "status": account.StatusPending,
		"expiresAt": bson.M{"$lte": value.CreatedAt},
	}); err != nil {
		return nil, err
	}
	doc := accountDocument{
		ID: value.ID, Username: value.Username, UsernameLower: strings.ToLower(value.Username),
		DisplayName: value.DisplayName, WebAuthnID: value.WebAuthnID,
		Status: account.StatusPending, BootstrapSlot: "initial",
		CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, ExpiresAt: &expiresAt,
	}
	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, appauth.ErrRegistrationClosed
		}
		return nil, err
	}
	return &appauth.User{Account: accountFromDocument(doc)}, nil
}

func (r *AccountRepository) FindPendingByID(ctx context.Context, id string, now time.Time) (*appauth.User, error) {
	return r.findUser(ctx, bson.M{"_id": id, "status": account.StatusPending, "expiresAt": bson.M{"$gt": now}})
}

func (r *AccountRepository) ActivateInitial(ctx context.Context, id string, credential webauthn.Credential, now time.Time) error {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"_id": id, "status": account.StatusPending, "expiresAt": bson.M{"$gt": now},
	}, bson.M{
		"$set": bson.M{
			"status": account.StatusActive, "updatedAt": now,
			"credentials": bson.A{passkeyCredentialDocument{ID: credentialID(credential.ID), Data: encoded}},
		},
		"$unset": bson.M{"expiresAt": ""},
	})
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return appauth.ErrCeremonyNotFound
	}
	return nil
}

func (r *AccountRepository) DeletePending(ctx context.Context, id string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id, "status": account.StatusPending})
	return err
}

func (r *AccountRepository) FindActiveByUserHandle(ctx context.Context, handle []byte) (*appauth.User, error) {
	return r.findUser(ctx, bson.M{"webAuthnId": handle, "status": account.StatusActive, "deletedAt": nil})
}

func (r *AccountRepository) UpdateCredential(ctx context.Context, accountID string, previous, credential webauthn.Credential, now time.Time) error {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	previousEncoded, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{
		"_id": accountID, "status": account.StatusActive,
		"credentials": bson.M{"$elemMatch": bson.M{
			"id": credentialID(credential.ID), "data": previousEncoded,
		}},
	}, bson.M{"$set": bson.M{"credentials.$.data": encoded, "updatedAt": now}})
	if err != nil {
		return err
	}
	if result.MatchedCount != 1 {
		return fmt.Errorf("passkey credential not found")
	}
	return nil
}

func (r *AccountRepository) FindByID(ctx context.Context, id string) (*account.Account, error) {
	var doc accountDocument
	err := r.collection.FindOne(ctx, bson.M{"_id": strings.TrimSpace(id)}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value := accountFromDocument(doc)
	return &value, nil
}

func (r *AccountRepository) HasActive(ctx context.Context) (bool, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{"status": account.StatusActive, "deletedAt": nil}, options.Count().SetLimit(1))
	return count > 0, err
}

func (r *AccountRepository) findUser(ctx context.Context, filter bson.M) (*appauth.User, error) {
	var doc accountDocument
	err := r.collection.FindOne(ctx, filter).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	credentials := make([]webauthn.Credential, 0, len(doc.Credentials))
	for _, stored := range doc.Credentials {
		var credential webauthn.Credential
		if err := json.Unmarshal(stored.Data, &credential); err != nil {
			return nil, fmt.Errorf("decode passkey credential %q: %w", stored.ID, err)
		}
		credentials = append(credentials, credential)
	}
	return &appauth.User{Account: accountFromDocument(doc), Credentials: credentials}, nil
}

func accountFromDocument(doc accountDocument) account.Account {
	return account.Account{
		ID: doc.ID, Username: doc.Username, DisplayName: doc.DisplayName,
		WebAuthnID: append([]byte(nil), doc.WebAuthnID...), Status: doc.Status,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, DeletedAt: doc.DeletedAt,
	}
}

func credentialID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}
