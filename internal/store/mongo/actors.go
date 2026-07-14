package mongostore

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

type ActorRepository struct {
	collection *mongo.Collection
}

type actorDocument struct {
	ID             string     `bson:"_id,omitempty"`
	OwnerAccountID string     `bson:"ownerAccountId,omitempty"`
	IsSystemActor  bool       `bson:"isSystemActor,omitempty"`
	Username       string     `bson:"username"`
	UsernameLower  string     `bson:"usernameLower"`
	Name           string     `bson:"name,omitempty"`
	Type           string     `bson:"type"`
	Host           *string    `bson:"host"`
	URI            string     `bson:"uri"`
	Inbox          string     `bson:"inbox"`
	SharedInbox    string     `bson:"sharedInbox"`
	FollowersURI   string     `bson:"followersUri,omitempty"`
	FollowingURI   string     `bson:"followingUri,omitempty"`
	FeaturedURI    string     `bson:"featuredUri,omitempty"`
	PublicKeyID    string     `bson:"publicKeyId"`
	PublicKeyPEM   string     `bson:"publicKeyPem"`
	PrivateKeyPEM  string     `bson:"privateKeyPem,omitempty"`
	IsSuspended    bool       `bson:"isSuspended"`
	DeletedAt      *time.Time `bson:"deletedAt,omitempty"`
}

func NewActorRepository(db *mongo.Database) *ActorRepository {
	return &ActorRepository{collection: db.Collection("actors")}
}

func (r *ActorRepository) EnsureLocalActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	if actor.UsernameLower == "" {
		actor.UsernameLower = strings.ToLower(actor.Username)
	}
	existing, err := r.FindLocalByUsername(ctx, actor.Username)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		if actor.IsSystemActor && !existing.IsSystemActor {
			_, err := r.collection.UpdateOne(ctx, bson.M{
				"_id":            existing.ID,
				"host":           nil,
				"ownerAccountId": bson.M{"$in": bson.A{nil, ""}},
			}, bson.M{"$set": bson.M{"isSystemActor": true}})
			if err != nil {
				return nil, err
			}
			return r.FindLocalByID(ctx, existing.ID)
		}
		return existing, nil
	}
	if actor.PublicKeyPEM == "" || actor.PrivateKeyPEM == "" {
		publicKey, privateKey, err := generateRSAKeyPair()
		if err != nil {
			return nil, err
		}
		actor.PublicKeyPEM = publicKey
		actor.PrivateKeyPEM = privateKey
	}
	doc := fromActor(actor)
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": actor.ID}, bson.M{"$setOnInsert": doc}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindLocalByID(ctx, actor.ID)
}

func (r *ActorRepository) FindOwnedLocalByID(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	accountID = strings.TrimSpace(accountID)
	actorID = strings.TrimSpace(actorID)
	if accountID == "" || actorID == "" {
		return nil, nil
	}
	return r.findOne(ctx, bson.M{
		"_id":            actorID,
		"ownerAccountId": accountID,
		"host":           nil,
		"isSuspended":    false,
	})
}

func (r *ActorRepository) FindLocalByID(ctx context.Context, id string) (*actors.Actor, error) {
	return r.findOne(ctx, bson.M{
		"_id":         id,
		"host":        nil,
		"isSuspended": false,
	})
}

func (r *ActorRepository) FindLocalByUsername(ctx context.Context, username string) (*actors.Actor, error) {
	return r.findOne(ctx, bson.M{
		"usernameLower": strings.ToLower(username),
		"host":          nil,
		"isSuspended":   false,
	})
}

func (r *ActorRepository) FindByURI(ctx context.Context, uri string) (*actors.Actor, error) {
	return r.findOne(ctx, bson.M{
		"uri":         uri,
		"isSuspended": false,
	})
}

func (r *ActorRepository) FindByPublicKeyID(ctx context.Context, keyID string) (*actors.Actor, error) {
	return r.findOne(ctx, bson.M{
		"publicKeyId": keyID,
		"isSuspended": false,
	})
}

func (r *ActorRepository) UpsertRemoteActor(ctx context.Context, actor actors.Actor) (*actors.Actor, error) {
	if actor.URI == "" {
		return nil, fmt.Errorf("remote actor uri is required")
	}
	if actor.Host == nil || *actor.Host == "" {
		return nil, fmt.Errorf("remote actor host is required")
	}
	actor.UsernameLower = strings.ToLower(actor.Username)
	doc := fromActor(actor)
	_, err := r.collection.UpdateOne(ctx, bson.M{"uri": actor.URI}, bson.M{
		"$set": bson.M{
			"username":      doc.Username,
			"usernameLower": doc.UsernameLower,
			"name":          doc.Name,
			"type":          doc.Type,
			"host":          doc.Host,
			"inbox":         doc.Inbox,
			"sharedInbox":   doc.SharedInbox,
			"followersUri":  doc.FollowersURI,
			"followingUri":  doc.FollowingURI,
			"featuredUri":   doc.FeaturedURI,
			"publicKeyId":   doc.PublicKeyID,
			"publicKeyPem":  doc.PublicKeyPEM,
			"isSuspended":   doc.IsSuspended,
		},
		"$setOnInsert": bson.M{
			"_id": doc.ID,
			"uri": doc.URI,
		},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindByURI(ctx, actor.URI)
}

func (r *ActorRepository) MarkRemoteActorDeleted(ctx context.Context, uri string) error {
	if uri == "" {
		return fmt.Errorf("remote actor uri is required")
	}
	now := time.Now().UTC()
	_, err := r.collection.UpdateOne(ctx, bson.M{"uri": uri, "host": bson.M{"$ne": nil}}, bson.M{
		"$set": bson.M{
			"isSuspended": true,
			"deletedAt":   now,
		},
	})
	return err
}

func (r *ActorRepository) findOne(ctx context.Context, filter bson.M) (*actors.Actor, error) {
	var doc actorDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &actors.Actor{
		ID:             doc.ID,
		OwnerAccountID: doc.OwnerAccountID,
		IsSystemActor:  doc.IsSystemActor,
		Username:       doc.Username,
		UsernameLower:  doc.UsernameLower,
		Name:           doc.Name,
		Type:           doc.Type,
		Host:           doc.Host,
		URI:            doc.URI,
		Inbox:          doc.Inbox,
		SharedInbox:    doc.SharedInbox,
		FollowersURI:   doc.FollowersURI,
		FollowingURI:   doc.FollowingURI,
		FeaturedURI:    doc.FeaturedURI,
		PublicKeyID:    doc.PublicKeyID,
		PublicKeyPEM:   doc.PublicKeyPEM,
		PrivateKeyPEM:  doc.PrivateKeyPEM,
		IsSuspended:    doc.IsSuspended,
	}, nil
}

func fromActor(actor actors.Actor) actorDocument {
	return actorDocument{
		ID:             actor.ID,
		OwnerAccountID: actor.OwnerAccountID,
		IsSystemActor:  actor.IsSystemActor,
		Username:       actor.Username,
		UsernameLower:  actor.UsernameLower,
		Name:           actor.Name,
		Type:           actor.Type,
		Host:           actor.Host,
		URI:            actor.URI,
		Inbox:          actor.Inbox,
		SharedInbox:    actor.SharedInbox,
		FollowersURI:   actor.FollowersURI,
		FollowingURI:   actor.FollowingURI,
		FeaturedURI:    actor.FeaturedURI,
		PublicKeyID:    actor.PublicKeyID,
		PublicKeyPEM:   actor.PublicKeyPEM,
		PrivateKeyPEM:  actor.PrivateKeyPEM,
		IsSuspended:    actor.IsSuspended,
	}
}

func generateRSAKeyPair() (string, string, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate actor keypair: %w", err)
	}
	privateDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privateDER})
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("marshal actor public key: %w", err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	return string(publicPEM), string(privatePEM), nil
}
