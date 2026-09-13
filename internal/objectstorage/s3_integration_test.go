package objectstorage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestS3RoundTrip(t *testing.T) {
	if os.Getenv("ROSMARINUS_S3_INTEGRATION") != "1" {
		t.Skip("set ROSMARINUS_S3_INTEGRATION=1 to test a local S3-compatible store")
	}
	ctx := context.Background()
	store, err := New(ctx, Config{Endpoint: "http://localhost:9000", Region: "us-east-1", Bucket: "rosmarinus", AccessKeyID: "rosmarinus", SecretAccessKey: "dummy-object-storage-password", PublicURL: "http://localhost:9000/rosmarinus", PathStyle: true, PresignTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Check(ctx); err != nil {
		t.Fatal(err)
	}
	body := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	object := Object{ContentType: "image/png", Size: int64(len(body)), SHA256: digest}

	key := uuid.NewString()
	signed, err := store.PresignPut(ctx, key, object)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, signed.URL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range signed.Headers {
		if http.CanonicalHeaderKey(name) == "Host" {
			if len(values) > 0 {
				req.Host = values[0]
			}
			continue
		}
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(response.Body)
		t.Fatalf("signed PUT status=%d body=%s", response.StatusCode, payload)
	}
	defer store.Delete(context.Background(), key)

	metadata, err := store.Stat(ctx, key)
	if err != nil || metadata != object {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	prefix, err := store.ReadPrefix(ctx, key, 8)
	if err != nil || !bytes.Equal(prefix, body[:8]) {
		t.Fatalf("prefix=%x err=%v", prefix, err)
	}
	publicResponse, err := http.Get(store.PublicURL(key))
	if err != nil {
		t.Fatal(err)
	}
	defer publicResponse.Body.Close()
	if publicResponse.StatusCode != http.StatusOK {
		t.Fatalf("public GET status=%d", publicResponse.StatusCode)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
}
