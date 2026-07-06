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
	if err != nil {
		return err
	}
	_, err = db.Collection("notes").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "uri", Value: 1}},
			Options: options.Index().
				SetName("uniq_notes_uri").
				SetUnique(true).
				SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "authorId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_notes_author_created_at"),
		},
	})
	if err != nil {
		return err
	}
	_, err = db.Collection("follows").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "followerId", Value: 1}, {Key: "followeeId", Value: 1}},
			Options: options.Index().
				SetName("uniq_follows_follower_followee").
				SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "followerId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_follows_follower_created_at"),
		},
		{
			Keys: bson.D{{Key: "followeeId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().
				SetName("idx_follows_followee_created_at"),
		},
	})
	return err
}
