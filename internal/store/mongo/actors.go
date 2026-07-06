package mongostore

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

type ActorRepository struct {
	collection *mongo.Collection
}

type actorDocument struct {
	ID            string  `bson:"_id,omitempty"`
	Username      string  `bson:"username"`
	UsernameLower string  `bson:"usernameLower"`
	Host          *string `bson:"host"`
	URI           string  `bson:"uri"`
	IsSuspended   bool    `bson:"isSuspended"`
}

func NewActorRepository(db *mongo.Database) *ActorRepository {
	return &ActorRepository{collection: db.Collection("actors")}
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

func (r *ActorRepository) findOne(ctx context.Context, filter bson.M) (*actors.Actor, error) {
	var doc actorDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &actors.Actor{
		ID:            doc.ID,
		Username:      doc.Username,
		UsernameLower: doc.UsernameLower,
		Host:          doc.Host,
		URI:           doc.URI,
		IsSuspended:   doc.IsSuspended,
	}, nil
}
