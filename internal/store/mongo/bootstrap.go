package mongostore

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func BootstrapIndexes(ctx context.Context, db *mongo.Database) error {
	_, err := db.Collection("actors").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "uri", Value: 1}},
			Options: options.Index().
				SetName("uniq_actors_uri").
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "usernameLower", Value: 1}, {Key: "host", Value: 1}},
			Options: options.Index().
				SetName("uniq_actors_username_host").
				SetUnique(true).
				SetSparse(true),
		},
	})
	return err
}
