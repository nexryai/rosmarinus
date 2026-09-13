package mediafetch

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"

	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
	"github.com/nexryai/rosmarinus/internal/objectstorage"
)

type uploadRepository struct{ record *domainmedia.Media }

func (r *uploadRepository) FindByID(_ context.Context, id string) (*domainmedia.Media, error) {
	if r.record != nil && r.record.ID == id {
		return r.record, nil
	}
	return nil, nil
}
func (r *uploadRepository) ListByOwner(context.Context, string) ([]domainmedia.Media, error) {
	return nil, nil
}
func (r *uploadRepository) CreatePendingLocal(_ context.Context, _, actorID, name, objectKey, publicURL, contentType string, size int64, digest string, width, height int) (*domainmedia.Media, error) {
	r.record = &domainmedia.Media{ID: "media-1", ObjectKey: objectKey, OwnerActorID: actorID, Name: name, PublicURL: publicURL, ContentType: contentType, Size: size, SHA256: digest, Width: width, Height: height, State: domainmedia.StatePending}
	return r.record, nil
}
func (r *uploadRepository) UpsertPending(context.Context, string, string, string) (*domainmedia.Media, error) {
	return nil, nil
}
func (r *uploadRepository) MarkReady(_ context.Context, _ string, contentType string, size int64, digest string) (*domainmedia.Media, error) {
	r.record.State, r.record.ContentType, r.record.Size, r.record.SHA256 = domainmedia.StateReady, contentType, size, digest
	return r.record, nil
}
func (r *uploadRepository) MarkFailed(_ context.Context, _, message string) error {
	r.record.State, r.record.Error = domainmedia.StateFailed, message
	return nil
}
func (r *uploadRepository) Delete(context.Context, string) error { r.record = nil; return nil }

type uploadStorage struct {
	object  objectstorage.Object
	body    []byte
	deleted bool
}

func (s *uploadStorage) Put(_ context.Context, _ string, source io.Reader, object objectstorage.Object) error {
	s.object = object
	s.body, _ = io.ReadAll(source)
	return nil
}
func (s *uploadStorage) Delete(context.Context, string) error { s.deleted = true; return nil }
func (s *uploadStorage) Stat(context.Context, string) (objectstorage.Object, error) {
	return s.object, nil
}
func (s *uploadStorage) ReadPrefix(_ context.Context, _ string, size int64) ([]byte, error) {
	return bytes.Clone(s.body[:min(int(size), len(s.body))]), nil
}
func (s *uploadStorage) PresignPut(_ context.Context, key string, object objectstorage.Object) (objectstorage.PresignedUpload, error) {
	s.object = object
	return objectstorage.PresignedUpload{URL: "https://upload.test/" + key}, nil
}
func (s *uploadStorage) PublicURL(key string) string { return "https://objects.test/" + key }

func TestUploadServiceUsesUUIDv4ObjectKey(t *testing.T) {
	repository, storage := &uploadRepository{}, &uploadStorage{}
	service := NewUploadService(repository, storage)
	record, signed, err := service.Prepare(context.Background(), "request-1", "actor-1", "photo.png", "image/png", 16, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := uuid.Parse(record.ObjectKey)
	if err != nil || parsed.Version() != 4 {
		t.Fatalf("object key = %q", record.ObjectKey)
	}
	if signed.URL != "https://upload.test/"+record.ObjectKey || record.PublicURL != "https://objects.test/"+record.ObjectKey {
		t.Fatalf("record=%+v upload=%+v", record, signed)
	}
}

func TestUploadServiceRejectsContentTypeMismatchAndDeletesObject(t *testing.T) {
	repository := &uploadRepository{record: &domainmedia.Media{ID: "media-1", ObjectKey: uuid.NewString(), OwnerActorID: "actor-1", ContentType: "image/png", Size: 4, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", State: domainmedia.StatePending}}
	storage := &uploadStorage{object: objectstorage.Object{ContentType: "image/png", Size: 4, SHA256: repository.record.SHA256}, body: []byte("text")}
	_, err := NewUploadService(repository, storage).Complete(context.Background(), "actor-1", "media-1")
	if err == nil || !storage.deleted || repository.record.State != domainmedia.StateFailed {
		t.Fatalf("err=%v deleted=%v media=%+v", err, storage.deleted, repository.record)
	}
}

func TestUploadServiceCompletesValidatedImage(t *testing.T) {
	body := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	repository := &uploadRepository{record: &domainmedia.Media{ID: "media-1", ObjectKey: uuid.NewString(), OwnerActorID: "actor-1", ContentType: "image/png", Size: int64(len(body)), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", State: domainmedia.StatePending}}
	storage := &uploadStorage{object: objectstorage.Object{ContentType: "image/png", Size: int64(len(body)), SHA256: repository.record.SHA256}, body: body}
	completed, err := NewUploadService(repository, storage).Complete(context.Background(), "actor-1", "media-1")
	if err != nil || completed.State != domainmedia.StateReady || storage.deleted {
		t.Fatalf("media=%+v deleted=%v err=%v", completed, storage.deleted, err)
	}
}
