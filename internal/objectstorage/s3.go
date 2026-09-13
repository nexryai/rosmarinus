package objectstorage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PublicURL       string
	PathStyle       bool
	PresignTTL      time.Duration
}

type Object struct {
	ContentType string
	Size        int64
	SHA256      string
}

type PresignedUpload struct {
	URL       string
	Headers   http.Header
	ExpiresAt time.Time
}

type Store interface {
	Put(context.Context, string, io.Reader, Object) error
	Delete(context.Context, string) error
	Stat(context.Context, string) (Object, error)
	ReadPrefix(context.Context, string, int64) ([]byte, error)
	PresignPut(context.Context, string, Object) (PresignedUpload, error)
	PublicURL(string) string
}

type S3 struct {
	bucket     string
	publicURL  string
	presignTTL time.Duration
	client     *s3.Client
	presigner  *s3.PresignClient
}

func New(ctx context.Context, cfg Config) (*S3, error) {
	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load object storage configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = cfg.PathStyle
		if cfg.Endpoint != "" {
			options.BaseEndpoint = aws.String(strings.TrimRight(cfg.Endpoint, "/"))
		}
	})
	return &S3{
		bucket: cfg.Bucket, publicURL: strings.TrimRight(cfg.PublicURL, "/"), presignTTL: cfg.PresignTTL,
		client: client, presigner: s3.NewPresignClient(client),
	}, nil
}

func (s *S3) Check(ctx context.Context) error {
	if _, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)}); err != nil {
		return fmt.Errorf("access object storage bucket %q: %w", s.bucket, err)
	}
	return nil
}

func (s *S3) Put(ctx context.Context, key string, source io.Reader, object Object) error {
	checksum, err := checksumBase64(object.SHA256)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), Body: source,
		ContentLength: aws.Int64(object.Size), ContentType: aws.String(object.ContentType),
		CacheControl: aws.String("public, max-age=31536000, immutable"), Metadata: map[string]string{"sha256": object.SHA256},
		ChecksumSHA256: aws.String(checksum),
	})
	return err
}

func (s *S3) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}

func (s *S3) Stat(ctx context.Context, key string) (Object, error) {
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), ChecksumMode: types.ChecksumModeEnabled})
	if err != nil {
		return Object{}, err
	}
	return Object{ContentType: aws.ToString(result.ContentType), Size: aws.ToInt64(result.ContentLength), SHA256: result.Metadata["sha256"]}, nil
}

func (s *S3) ReadPrefix(ctx context.Context, key string, size int64) ([]byte, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), Range: aws.String(fmt.Sprintf("bytes=0-%d", size-1))})
	if err != nil {
		return nil, err
	}
	defer result.Body.Close()
	return io.ReadAll(io.LimitReader(result.Body, size))
}

func (s *S3) PresignPut(ctx context.Context, key string, object Object) (PresignedUpload, error) {
	checksum, err := checksumBase64(object.SHA256)
	if err != nil {
		return PresignedUpload{}, err
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket), Key: aws.String(key), ContentLength: aws.Int64(object.Size),
		ContentType: aws.String(object.ContentType), CacheControl: aws.String("public, max-age=31536000, immutable"), Metadata: map[string]string{"sha256": object.SHA256},
		ChecksumSHA256: aws.String(checksum),
	}
	result, err := s.presigner.PresignPutObject(ctx, input, func(options *s3.PresignOptions) { options.Expires = s.presignTTL })
	if err != nil {
		return PresignedUpload{}, err
	}
	return PresignedUpload{URL: result.URL, Headers: result.SignedHeader, ExpiresAt: time.Now().UTC().Add(s.presignTTL)}, nil
}

func (s *S3) PublicURL(key string) string {
	return s.publicURL + "/" + key
}

func checksumBase64(digest string) (string, error) {
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != sha256.Size {
		return "", fmt.Errorf("SHA-256 digest must contain 64 hexadecimal characters")
	}
	return base64.StdEncoding.EncodeToString(decoded), nil
}
