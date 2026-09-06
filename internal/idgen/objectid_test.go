package idgen

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestNewUniqueObjectIDReturnsHexString(t *testing.T) {
	id, err := NewUniqueObjectID(context.Background(), func(context.Context, string) (bool, error) { return false, nil })
	if err != nil {
		t.Fatalf("NewUniqueObjectID returned error: %v", err)
	}
	if len(id) != 24 {
		t.Fatalf("ID length = %d, want 24", len(id))
	}
	if _, err := bson.ObjectIDFromHex(id); err != nil {
		t.Fatalf("ID is not ObjectID hex: %q: %v", id, err)
	}
}

func TestNewUniqueObjectIDRetriesCollision(t *testing.T) {
	values := []string{"000000000000000000000001", "000000000000000000000002"}
	index := 0
	id, err := newUniqueObjectID(context.Background(), func(_ context.Context, id string) (bool, error) {
		return id == values[0], nil
	}, func() string {
		value := values[index]
		index++
		return value
	})
	if err != nil {
		t.Fatalf("newUniqueObjectID returned error: %v", err)
	}
	if id != values[1] || index != 2 {
		t.Fatalf("ID = %q attempts = %d", id, index)
	}
}

func TestNewUniqueObjectIDReturnsCollisionLookupError(t *testing.T) {
	want := errors.New("lookup failed")
	_, err := newUniqueObjectID(context.Background(), func(context.Context, string) (bool, error) { return false, want }, func() string { return "000000000000000000000001" })
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped lookup error", err)
	}
}
