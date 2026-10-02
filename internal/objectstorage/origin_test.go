package objectstorage

import (
	"context"
	"testing"
)

func TestUploadOriginMatchesPresignedBucketEndpoint(t *testing.T) {
	for _, test := range []struct {
		name, endpoint, region, bucket, want string
		pathStyle                            bool
	}{
		{name: "path style", endpoint: "https://s3.example/storage", region: "us-east-1", bucket: "images", pathStyle: true, want: "https://s3.example"},
		{name: "virtual hosted", endpoint: "https://s3.example", region: "us-east-1", bucket: "images", want: "https://images.s3.example"},
		{name: "AWS default", region: "ap-northeast-1", bucket: "images", want: "https://images.s3.ap-northeast-1.amazonaws.com"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := New(context.Background(), Config{Endpoint: test.endpoint, Region: test.region, Bucket: test.bucket, PathStyle: test.pathStyle, AccessKeyID: "test", SecretAccessKey: "test"})
			if err != nil {
				t.Fatal(err)
			}
			origin, err := store.UploadOrigin(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if origin != test.want {
				t.Fatalf("origin=%q want=%q", origin, test.want)
			}
		})
	}
}
