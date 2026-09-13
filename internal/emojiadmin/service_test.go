package emojiadmin

import (
	"context"
	"io"
	"log"
	"testing"

	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
	mediafetch "github.com/nexryai/rosmarinus/internal/media"
)

type fakeEmojiRepository struct {
	items map[string]emojis.Emoji
}

func (r *fakeEmojiRepository) CreateLocal(_ context.Context, value emojis.Emoji) (*emojis.Emoji, error) {
	for _, item := range r.items {
		if item.Host == "" && item.Name == value.Name {
			return nil, emojis.ErrNameConflict
		}
	}
	value.ID = "local-created"
	r.items[value.ID] = value
	return &value, nil
}

func (r *fakeEmojiRepository) UpdateLocal(_ context.Context, id string, value emojis.Emoji) (*emojis.Emoji, error) {
	value.ID = id
	r.items[id] = value
	return &value, nil
}

func (r *fakeEmojiRepository) DeleteLocal(_ context.Context, id string) error {
	delete(r.items, id)
	return nil
}

func (r *fakeEmojiRepository) FindByID(_ context.Context, id string) (*emojis.Emoji, error) {
	value, ok := r.items[id]
	if !ok {
		return nil, nil
	}
	return &value, nil
}

func (r *fakeEmojiRepository) FindLocalByName(_ context.Context, name string) (*emojis.Emoji, error) {
	for _, item := range r.items {
		if item.Host == "" && item.Name == name {
			return &item, nil
		}
	}
	return nil, nil
}

type fakeMediaRepository struct {
	items      map[string]domainmedia.Media
	storedBody string
}

func (r *fakeMediaRepository) FindByID(_ context.Context, id string) (*domainmedia.Media, error) {
	value, ok := r.items[id]
	if !ok {
		return nil, nil
	}
	return &value, nil
}

func (r *fakeMediaRepository) CreateLocal(_ context.Context, _, actorID, name, publicBaseURL, contentType string, size int64, digest string, _, _ int, source io.Reader) (*domainmedia.Media, error) {
	body, _ := io.ReadAll(source)
	r.storedBody = string(body)
	return &domainmedia.Media{ID: "media-imported", OwnerActorID: actorID, Name: name, PublicURL: publicBaseURL + "/media-imported", ContentType: contentType, Size: size, SHA256: digest, State: domainmedia.StateReady}, nil
}

type fakeFetcher struct {
	url string
}

func (f *fakeFetcher) Fetch(_ context.Context, rawURL string) (mediafetch.Result, error) {
	f.url = rawURL
	return mediafetch.Result{Body: []byte("remote image"), ContentType: "image/webp"}, nil
}

func TestCreateFromMediaRequiresReadyMediaOwnedByActor(t *testing.T) {
	repository := &fakeEmojiRepository{items: map[string]emojis.Emoji{}}
	media := &fakeMediaRepository{items: map[string]domainmedia.Media{
		"foreign": {ID: "foreign", OwnerActorID: "actor-2", PublicURL: "https://local.test/media/foreign", ContentType: "image/png", State: domainmedia.StateReady},
	}}
	service := New(repository, media, nil, "https://local.test", log.New(io.Discard, "", 0))

	if _, err := service.CreateFromMedia(context.Background(), "actor-1", "party", "foreign"); err == nil {
		t.Fatal("CreateFromMedia accepted another Actor's media")
	}
}

func TestCreateFromMediaRegistersLocalActivityPubEmoji(t *testing.T) {
	repository := &fakeEmojiRepository{items: map[string]emojis.Emoji{}}
	media := &fakeMediaRepository{items: map[string]domainmedia.Media{
		"media-1": {ID: "media-1", OwnerActorID: "actor-1", PublicURL: "https://local.test/media/1", ContentType: "image/png", State: domainmedia.StateReady},
	}}
	service := New(repository, media, nil, "https://local.test", log.New(io.Discard, "", 0))

	created, err := service.CreateFromMedia(context.Background(), "actor-1", "rosemary", "media-1")
	if err != nil {
		t.Fatalf("CreateFromMedia returned error: %v", err)
	}
	if created.Name != "rosemary" || created.URI != "https://local.test/emojis/rosemary" || created.PublicURL != "https://local.test/media/1" || created.MediaType != "image/png" {
		t.Fatalf("unexpected local emoji: %+v", created)
	}
}

func TestImportRemoteCopiesValidatedBytesToLocalMedia(t *testing.T) {
	repository := &fakeEmojiRepository{items: map[string]emojis.Emoji{
		"remote-1": {ID: "remote-1", Host: "remote.test", Name: "party", OriginalURL: "https://remote.test/party.webp"},
	}}
	media := &fakeMediaRepository{items: map[string]domainmedia.Media{}}
	fetcher := &fakeFetcher{}
	service := New(repository, media, fetcher, "https://local.test", log.New(io.Discard, "", 0))

	created, err := service.ImportRemote(context.Background(), "actor-1", "remote-1", "party_here")
	if err != nil {
		t.Fatalf("ImportRemote returned error: %v", err)
	}
	if fetcher.url != "https://remote.test/party.webp" || media.storedBody != "remote image" {
		t.Fatalf("remote image was not copied: url=%q body=%q", fetcher.url, media.storedBody)
	}
	if created.Name != "party_here" || created.Host != "" || created.PublicURL != "https://local.test/media/media-imported" || created.URI != "https://local.test/emojis/party_here" {
		t.Fatalf("unexpected imported emoji: %+v", created)
	}
}
