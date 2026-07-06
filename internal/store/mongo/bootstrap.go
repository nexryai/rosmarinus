package mongostore

import (
	"context"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

func BootstrapIndexes(ctx context.Context, db *mongo.Database) error {
	_ = ctx
	_ = db
	return nil
}
