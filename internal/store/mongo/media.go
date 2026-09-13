package mongostore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
)

type MediaRepository struct{ collection *mongo.Collection }

type mediaDocument struct {
	ID           string     `bson:"_id"`
	UploadKey    string     `bson:"uploadKey,omitempty"`
	ObjectKey    string     `bson:"objectKey"`
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

func NewMediaRepository(db *mongo.Database) *MediaRepository {
	return &MediaRepository{collection: db.Collection("media_objects")}
}

func (r *MediaRepository) CreatePendingLocal(ctx context.Context, uploadKey, actorID, name, objectKey, publicURL, contentType string, size int64, digest string, width, height int) (*domainmedia.Media, error) {
	if uploadKey == "" || actorID == "" || objectKey == "" || publicURL == "" || size <= 0 || digest == "" {
		return nil, fmt.Errorf("local media metadata is incomplete")
	}
	id, err := newDocumentID(ctx, r.collection)
	if err != nil {
		return nil, fmt.Errorf("generate local media id: %w", err)
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{"uploadKey": uploadKey}, bson.M{"$setOnInsert": bson.M{
		"_id": id, "uploadKey": uploadKey, "objectKey": objectKey, "ownerActorId": actorID, "name": name,
		"width": width, "height": height, "originalUrl": publicURL, "publicUrl": publicURL,
		"contentType": contentType, "size": size, "sha256": digest, "state": domainmedia.StatePending, "createdAt": time.Now().UTC(),
	}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			existing, findErr := r.findOne(ctx, bson.M{"uploadKey": uploadKey})
			if findErr == nil && existing != nil && existing.OwnerActorID == actorID && existing.SHA256 == digest && existing.Size == size && existing.ContentType == contentType {
				return existing, nil
			}
			return nil, domainmedia.ErrObjectKeyConflict
		}
		return nil, err
	}
	if result.MatchedCount > 0 {
		existing, findErr := r.findOne(ctx, bson.M{"uploadKey": uploadKey})
		if findErr != nil {
			return nil, findErr
		}
		if existing == nil || existing.OwnerActorID != actorID || existing.SHA256 != digest || existing.Size != size || existing.ContentType != contentType {
			return nil, fmt.Errorf("local media id conflicts with an existing upload")
		}
		return existing, nil
	}
	return r.FindByID(ctx, id)
}

func (r *MediaRepository) FindByID(ctx context.Context, id string) (*domainmedia.Media, error) {
	return r.findOne(ctx, bson.M{"_id": id})
}

func (r *MediaRepository) ListByOwner(ctx context.Context, actorID string) ([]domainmedia.Media, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"ownerActorId": actorID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var documents []mediaDocument
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, err
	}
	result := make([]domainmedia.Media, 0, len(documents))
	for _, document := range documents {
		result = append(result, *toMedia(document))
	}
	return result, nil
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

func (r *MediaRepository) UpsertPending(ctx context.Context, originalURL, objectKey, publicURL string) (*domainmedia.Media, error) {
	if originalURL == "" || objectKey == "" || publicURL == "" {
		return nil, fmt.Errorf("media source, object key, and public URL are required")
	}
	existing, err := r.findOne(ctx, bson.M{"originalUrl": originalURL})
	if err != nil || existing != nil {
		return existing, err
	}
	id, err := newDocumentID(ctx, r.collection)
	if err != nil {
		return nil, fmt.Errorf("generate remote media id: %w", err)
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"originalUrl": originalURL}, bson.M{"$setOnInsert": bson.M{
		"_id": id, "objectKey": objectKey, "originalUrl": originalURL, "publicUrl": publicURL,
		"state": domainmedia.StatePending, "createdAt": time.Now().UTC(),
	}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			existing, findErr := r.findOne(ctx, bson.M{"originalUrl": originalURL})
			if findErr == nil && existing != nil {
				return existing, nil
			}
			return nil, domainmedia.ErrObjectKeyConflict
		}
		return nil, err
	}
	return r.findOne(ctx, bson.M{"originalUrl": originalURL})
}

func (r *MediaRepository) MarkReady(ctx context.Context, id, contentType string, size int64, digest string) (*domainmedia.Media, error) {
	now := time.Now().UTC()
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
		"contentType": contentType, "size": size, "sha256": digest, "state": domainmedia.StateReady, "error": "", "fetchedAt": now,
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
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"state": domainmedia.StateFailed, "error": message}})
	return err
}

func (r *MediaRepository) Delete(ctx context.Context, id string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func toMedia(doc mediaDocument) *domainmedia.Media {
	return &domainmedia.Media{ID: doc.ID, ObjectKey: doc.ObjectKey, OwnerActorID: doc.OwnerActorID, Name: doc.Name,
		Width: doc.Width, Height: doc.Height, OriginalURL: doc.OriginalURL, PublicURL: doc.PublicURL,
		ContentType: doc.ContentType, Size: doc.Size, SHA256: doc.SHA256, State: doc.State,
		Error: doc.Error, CreatedAt: doc.CreatedAt, FetchedAt: doc.FetchedAt}
}
