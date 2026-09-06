package idgen

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const maxObjectIDAttempts = 8

type ExistsFunc func(context.Context, string) (bool, error)

func NewUniqueObjectID(ctx context.Context, exists ExistsFunc) (string, error) {
	if exists == nil {
		return "", fmt.Errorf("object id collision lookup is required")
	}
	return newUniqueObjectID(ctx, exists, func() string { return bson.NewObjectID().Hex() })
}

func newUniqueObjectID(ctx context.Context, exists ExistsFunc, generate func() string) (string, error) {
	for range maxObjectIDAttempts {
		id := generate()
		duplicate, err := exists(ctx, id)
		if err != nil {
			return "", fmt.Errorf("check object id collision: %w", err)
		}
		if !duplicate {
			return id, nil
		}
	}
	return "", fmt.Errorf("generate unique object id after %d attempts", maxObjectIDAttempts)
}
