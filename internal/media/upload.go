package mediafetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
	"github.com/nexryai/rosmarinus/internal/objectstorage"
)

type UploadService struct {
	repository domainmedia.Repository
	storage    objectstorage.Store
}

func NewUploadService(repository domainmedia.Repository, storage objectstorage.Store) *UploadService {
	return &UploadService{repository: repository, storage: storage}
}

func (s *UploadService) FindByID(ctx context.Context, id string) (*domainmedia.Media, error) {
	return s.repository.FindByID(ctx, id)
}

func (s *UploadService) Prepare(ctx context.Context, uploadKey, actorID, name, contentType string, size int64, digest string, width, height int) (*domainmedia.Media, objectstorage.PresignedUpload, error) {
	record, err := s.createPending(ctx, uploadKey, actorID, name, contentType, size, digest, width, height)
	if err != nil {
		return nil, objectstorage.PresignedUpload{}, err
	}
	if record.State == domainmedia.StateReady {
		return record, objectstorage.PresignedUpload{}, nil
	}
	upload, err := s.storage.PresignPut(ctx, record.ObjectKey, objectstorage.Object{ContentType: record.ContentType, Size: record.Size, SHA256: record.SHA256})
	if err != nil {
		return nil, objectstorage.PresignedUpload{}, err
	}
	return record, upload, nil
}

func (s *UploadService) Complete(ctx context.Context, actorID, id string) (*domainmedia.Media, error) {
	record, err := s.owned(ctx, actorID, id)
	if err != nil {
		return nil, err
	}
	if record.State == domainmedia.StateReady {
		return record, nil
	}
	stored, err := s.storage.Stat(ctx, record.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("inspect uploaded object: %w", err)
	}
	if stored.Size != record.Size || stored.ContentType != record.ContentType || !strings.EqualFold(stored.SHA256, record.SHA256) {
		_ = s.storage.Delete(ctx, record.ObjectKey)
		_ = s.repository.MarkFailed(ctx, record.ID, "uploaded object metadata does not match")
		return nil, domainmedia.ErrInvalidObject
	}
	prefix, err := s.storage.ReadPrefix(ctx, record.ObjectKey, 512)
	if err != nil || http.DetectContentType(prefix) != record.ContentType {
		_ = s.storage.Delete(ctx, record.ObjectKey)
		_ = s.repository.MarkFailed(ctx, record.ID, "uploaded object content does not match its media type")
		return nil, domainmedia.ErrInvalidObject
	}
	return s.repository.MarkReady(ctx, record.ID, record.ContentType, record.Size, record.SHA256)
}

func (s *UploadService) Store(ctx context.Context, uploadKey, actorID, name, contentType string, size int64, digest string, width, height int, source io.Reader) (*domainmedia.Media, error) {
	record, err := s.createPending(ctx, uploadKey, actorID, name, contentType, size, digest, width, height)
	if err != nil {
		return nil, err
	}
	if record.State == domainmedia.StateReady {
		return record, nil
	}
	object := objectstorage.Object{ContentType: record.ContentType, Size: record.Size, SHA256: record.SHA256}
	if err := s.storage.Put(ctx, record.ObjectKey, source, object); err != nil {
		_ = s.repository.MarkFailed(ctx, record.ID, err.Error())
		return nil, err
	}
	return s.repository.MarkReady(ctx, record.ID, record.ContentType, record.Size, record.SHA256)
}

func (s *UploadService) createPending(ctx context.Context, uploadKey, actorID, name, contentType string, size int64, digest string, width, height int) (*domainmedia.Media, error) {
	for range 8 {
		objectKey := uuid.NewString()
		record, err := s.repository.CreatePendingLocal(ctx, uploadKey, actorID, filepath.Base(name), objectKey, s.storage.PublicURL(objectKey), contentType, size, strings.ToLower(digest), width, height)
		if !errors.Is(err, domainmedia.ErrObjectKeyConflict) {
			return record, err
		}
	}
	return nil, fmt.Errorf("generate unique media object key: %w", domainmedia.ErrObjectKeyConflict)
}

func (s *UploadService) Delete(ctx context.Context, actorID, id string) error {
	record, err := s.owned(ctx, actorID, id)
	if err != nil {
		return err
	}
	if err := s.storage.Delete(ctx, record.ObjectKey); err != nil {
		return err
	}
	return s.repository.Delete(ctx, record.ID)
}

func (s *UploadService) owned(ctx context.Context, actorID, id string) (*domainmedia.Media, error) {
	record, err := s.repository.FindByID(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	// Empty actorID is reserved for trusted backend cleanup such as deleting a managed emoji.
	if record == nil || (actorID != "" && record.OwnerActorID != actorID) {
		return nil, domainmedia.ErrNotFound
	}
	return record, nil
}
