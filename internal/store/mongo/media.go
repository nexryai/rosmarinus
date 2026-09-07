package mongostore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
)

type MediaRepository struct {
	collection *mongo.Collection
	bucket     *mongo.GridFSBucket
}

type mediaDocument struct {
	ID           string     `bson:"_id"`
	LegacyIDs    []string   `bson:"legacyIds,omitempty"`
	UploadKey    string     `bson:"uploadKey,omitempty"`
	OwnerActorID string     `bson:"ownerActorId,omitempty"`
	Name         string     `bson:"name,omitempty"`
	Width        int        `bson:"width,omitempty"`
	Height       int        `bson:"height,omitempty"`
	OriginalURL  string     `bson:"originalUrl"`
	PublicURL    string     `bson:"publicUrl"`
	ContentType  string     `bson:"contentType,omitempty"`
	Size         int64      `bson:"size,omitempty"`
	SHA256       string     `bson:"sha256,omitempty"`
	State        string     `bson:"state"`
	Error        string     `bson:"error,omitempty"`
	CreatedAt    time.Time  `bson:"createdAt"`
	FetchedAt    *time.Time `bson:"fetchedAt,omitempty"`
}

func (r *MediaRepository) CreateLocal(ctx context.Context, uploadKey, actorID, name, publicBaseURL, contentType string, size int64, digest string, width, height int, source io.Reader) (*domainmedia.Media, error) {
	if uploadKey == "" || actorID == "" || publicBaseURL == "" || size <= 0 || digest == "" {
		return nil, fmt.Errorf("local media metadata is incomplete")
	}
	id, err := newDocumentID(ctx, r.collection)
	if err != nil {
		return nil, fmt.Errorf("generate local media id: %w", err)
	}
	publicURL := strings.TrimRight(publicBaseURL, "/") + "/" + id
	now := time.Now().UTC()
	result, err := r.collection.UpdateOne(ctx, bson.M{"uploadKey": uploadKey}, bson.M{"$setOnInsert": bson.M{
		"_id": id, "uploadKey": uploadKey, "ownerActorId": actorID, "name": name, "width": width, "height": height, "originalUrl": publicURL,
		"publicUrl": publicURL, "state": domainmedia.StatePending, "createdAt": now,
	}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	if result.MatchedCount > 0 {
		existing, findErr := r.findOne(ctx, bson.M{"uploadKey": uploadKey})
		if findErr != nil {
			return nil, findErr
		}
		if existing == nil || existing.OwnerActorID != actorID || (existing.SHA256 != "" && existing.SHA256 != digest) {
			return nil, fmt.Errorf("local media id conflicts with an existing upload")
		}
		if existing.State == domainmedia.StateReady {
			return existing, nil
		}
		id = existing.ID
	}
	if err := r.StoreBlob(ctx, id, source, contentType, size, digest); err != nil {
		_ = r.MarkFailed(ctx, id, "store local upload")
		return nil, err
	}
	return r.MarkReady(ctx, id, contentType, size, digest)
}

func NewMediaRepository(db *mongo.Database) *MediaRepository {
	return &MediaRepository{
		collection: db.Collection("media"),
		bucket:     db.GridFSBucket(options.GridFSBucket().SetName("media_fs")),
	}
}

func (r *MediaRepository) FindByID(ctx context.Context, id string) (*domainmedia.Media, error) {
	return r.findOne(ctx, bson.M{"$or": bson.A{bson.M{"_id": id}, bson.M{"legacyIds": id}}})
}

func (r *MediaRepository) findOne(ctx context.Context, filter bson.M) (*domainmedia.Media, error) {
	var doc mediaDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toMedia(doc), nil
}

func (r *MediaRepository) UpsertPending(ctx context.Context, originalURL, publicURL string) (*domainmedia.Media, error) {
	if originalURL == "" || publicURL == "" {
		return nil, fmt.Errorf("media original and public urls are required")
	}
	existing, err := r.findOne(ctx, bson.M{"originalUrl": originalURL})
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	id, err := newDocumentID(ctx, r.collection)
	if err != nil {
		return nil, fmt.Errorf("generate remote media id: %w", err)
	}
	publicURL = strings.TrimRight(publicURL, "/") + "/" + id
	now := time.Now().UTC()
	_, err = r.collection.UpdateOne(ctx, bson.M{"originalUrl": originalURL}, bson.M{
		"$setOnInsert": bson.M{
			"_id": id, "originalUrl": originalURL, "publicUrl": publicURL,
			"state": domainmedia.StatePending, "createdAt": now,
		},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.findOne(ctx, bson.M{"originalUrl": originalURL})
}

func (r *MediaRepository) StoreBlob(ctx context.Context, id string, source io.Reader, contentType string, size int64, digest string) error {
	if err := r.bucket.Delete(ctx, id); err != nil && !errors.Is(err, mongo.ErrFileNotFound) {
		return err
	}
	metadata := bson.D{{Key: "contentType", Value: contentType}, {Key: "size", Value: size}, {Key: "sha256", Value: digest}}
	err := r.bucket.UploadFromStreamWithID(ctx, id, id, source, options.GridFSUpload().SetMetadata(metadata))
	return err
}

func (r *MediaRepository) OpenBlob(ctx context.Context, id string) (io.ReadCloser, error) {
	return r.bucket.OpenDownloadStream(ctx, id)
}

func (r *MediaRepository) MarkReady(ctx context.Context, id, contentType string, size int64, digest string) (*domainmedia.Media, error) {
	now := time.Now().UTC()
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"contentType": contentType, "size": size, "sha256": digest,
		"state": domainmedia.StateReady, "error": "", "fetchedAt": now,
	}})
	if err != nil {
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, fmt.Errorf("media record not found: %s", id)
	}
	return r.FindByID(ctx, id)
}

func (r *MediaRepository) MarkFailed(ctx context.Context, id, message string) error {
	message = strings.TrimSpace(message)
	if runes := []rune(message); len(runes) > 512 {
		message = string(runes[:512])
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"state": domainmedia.StateFailed, "error": message,
	}})
	return err
}

func toMedia(doc mediaDocument) *domainmedia.Media {
	return &domainmedia.Media{
		ID: doc.ID, OwnerActorID: doc.OwnerActorID, Name: doc.Name, Width: doc.Width, Height: doc.Height, OriginalURL: doc.OriginalURL, PublicURL: doc.PublicURL,
		ContentType: doc.ContentType, Size: doc.Size, SHA256: doc.SHA256,
		State: doc.State, Error: doc.Error, CreatedAt: doc.CreatedAt, FetchedAt: doc.FetchedAt,
	}
}
