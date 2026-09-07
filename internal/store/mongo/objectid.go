package mongostore

import (
	"context"
	"fmt"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/nexryai/rosmarinus/internal/idgen"
)

func newDocumentID(ctx context.Context, collection *mongo.Collection) (string, error) {
	return idgen.NewUniqueObjectID(ctx, func(ctx context.Context, id string) (bool, error) {
		count, err := collection.CountDocuments(ctx, bson.M{"$or": bson.A{
			bson.M{"_id": id},
			bson.M{"legacyIds": id},
		}})
		return count != 0, err
	})
}

func requireDocumentID(field, id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("%s is required", field)
	}
	if id != trimmed || len(id) != 24 || id != strings.ToLower(id) {
		return fmt.Errorf("%s must be a lowercase ObjectID string", field)
	}
	if _, err := bson.ObjectIDFromHex(id); err != nil {
		return fmt.Errorf("%s must be a lowercase ObjectID string", field)
	}
	return nil
}
