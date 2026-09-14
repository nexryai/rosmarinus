package mongostore

import (
	"bytes"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestActiveMuteFilterIncludesIndefiniteAndFutureMutes(t *testing.T) {
	now := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	filter := activeMuteFilter("viewer-1", now)
	encoded, err := bson.MarshalExtJSON(filter, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"muterId":"viewer-1"`)) || !bytes.Contains(encoded, []byte(`"expiresAt":null`)) || !bytes.Contains(encoded, []byte(`"$gt"`)) {
		t.Fatalf("active mute filter = %s", encoded)
	}
}

func TestNoteListPipelineFiltersMutedRenoteTargetsOnlyForTimelines(t *testing.T) {
	plain, err := bson.MarshalExtJSON(bson.M{"pipeline": noteListPipeline(bson.M{"deletedAt": nil}, 20, -1, nil)}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	muted, err := bson.MarshalExtJSON(bson.M{"pipeline": noteListPipeline(bson.M{"deletedAt": nil}, 20, -1, []string{"muted-1"})}, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(plain, []byte(`"renoteTarget"`)) {
		t.Fatalf("non-timeline pipeline unexpectedly filters mutes: %s", plain)
	}
	if !bytes.Contains(muted, []byte(`"$lookup"`)) || !bytes.Contains(muted, []byte(`"renoteTarget.authorId"`)) || !bytes.Contains(muted, []byte(`"muted-1"`)) {
		t.Fatalf("timeline mute pipeline = %s", muted)
	}
}
