package emojiadmin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/url"
	"path"
	"strings"

	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	domainmedia "github.com/nexryai/rosmarinus/internal/domain/media"
	mediafetch "github.com/nexryai/rosmarinus/internal/media"
)

type EmojiRepository interface {
	CreateLocal(context.Context, emojis.Emoji) (*emojis.Emoji, error)
	UpdateLocal(context.Context, string, emojis.Emoji) (*emojis.Emoji, error)
	DeleteLocal(context.Context, string) error
	FindByID(context.Context, string) (*emojis.Emoji, error)
	FindLocalByName(context.Context, string) (*emojis.Emoji, error)
}

type MediaRepository interface {
	CreateLocal(context.Context, string, string, string, string, string, int64, string, int, int, io.Reader) (*domainmedia.Media, error)
	FindByID(context.Context, string) (*domainmedia.Media, error)
}

type MediaFetcher interface {
	Fetch(context.Context, string) (mediafetch.Result, error)
}

type Service struct {
	emojis    EmojiRepository
	media     MediaRepository
	fetcher   MediaFetcher
	publicURL string
	logger    *log.Logger
}

func New(repository EmojiRepository, media MediaRepository, fetcher MediaFetcher, publicURL string, logger *log.Logger) *Service {
	return &Service{emojis: repository, media: media, fetcher: fetcher, publicURL: strings.TrimRight(publicURL, "/"), logger: logger}
}

func (s *Service) CreateFromMedia(ctx context.Context, actorID, name, mediaID string) (*emojis.Emoji, error) {
	media, err := s.ownedReadyMedia(ctx, actorID, mediaID)
	if err != nil {
		return nil, err
	}
	created, err := s.emojis.CreateLocal(ctx, s.localEmoji(name, media))
	if err == nil && s.logger != nil {
		s.logger.Printf("emoji: local emoji created id=%s name=%s media_id=%s", created.ID, created.Name, media.ID)
	}
	return created, err
}

func (s *Service) Update(ctx context.Context, actorID, id, name, mediaID string) (*emojis.Emoji, error) {
	current, err := s.emojis.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Host != "" {
		return nil, emojis.ErrNotFound
	}
	updated := *current
	updated.Name = name
	updated.URI = s.emojiURI(name)
	if strings.TrimSpace(mediaID) != "" {
		media, err := s.ownedReadyMedia(ctx, actorID, mediaID)
		if err != nil {
			return nil, err
		}
		updated.OriginalURL, updated.PublicURL, updated.MediaType = media.PublicURL, media.PublicURL, media.ContentType
	}
	result, err := s.emojis.UpdateLocal(ctx, id, updated)
	if err == nil && s.logger != nil {
		s.logger.Printf("emoji: local emoji updated id=%s name=%s", result.ID, result.Name)
	}
	return result, err
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if err := s.emojis.DeleteLocal(ctx, id); err != nil {
		return err
	}
	if s.logger != nil {
		s.logger.Printf("emoji: local emoji deleted id=%s", id)
	}
	return nil
}

func (s *Service) ImportRemote(ctx context.Context, actorID, sourceID, name string) (*emojis.Emoji, error) {
	source, err := s.emojis.FindByID(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if source == nil || source.Host == "" {
		return nil, emojis.ErrNotFound
	}
	if strings.TrimSpace(name) == "" {
		name = source.Name
	}
	if existing, err := s.emojis.FindLocalByName(ctx, name); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, emojis.ErrNameConflict
	}
	if s.fetcher == nil || s.media == nil {
		return nil, fmt.Errorf("emoji import media services are not configured")
	}
	fetched, err := s.fetcher.Fetch(ctx, source.OriginalURL)
	if err != nil {
		return nil, fmt.Errorf("fetch remote emoji: %w", err)
	}
	digestBytes := sha256.Sum256(fetched.Body)
	digest := hex.EncodeToString(digestBytes[:])
	filename := remoteFilename(source.OriginalURL, name)
	media, err := s.media.CreateLocal(ctx, "emoji-import\x00"+source.ID+"\x00"+name, actorID, filename, s.publicURL+"/media", fetched.ContentType, int64(len(fetched.Body)), digest, 0, 0, bytes.NewReader(fetched.Body))
	if err != nil {
		return nil, fmt.Errorf("store imported emoji: %w", err)
	}
	created, err := s.emojis.CreateLocal(ctx, s.localEmoji(name, media))
	if err == nil && s.logger != nil {
		s.logger.Printf("emoji: remote emoji imported source_id=%s id=%s name=%s", source.ID, created.ID, created.Name)
	}
	return created, err
}

func (s *Service) ownedReadyMedia(ctx context.Context, actorID, mediaID string) (*domainmedia.Media, error) {
	if s.media == nil {
		return nil, fmt.Errorf("emoji media service is not configured")
	}
	media, err := s.media.FindByID(ctx, strings.TrimSpace(mediaID))
	if err != nil {
		return nil, err
	}
	if media == nil || media.OwnerActorID != actorID || media.State != domainmedia.StateReady || media.PublicURL == "" {
		return nil, fmt.Errorf("ready media owned by the selected Actor is required")
	}
	if !strings.HasPrefix(media.ContentType, "image/") {
		return nil, fmt.Errorf("emoji media must be an image")
	}
	return media, nil
}

func (s *Service) localEmoji(name string, media *domainmedia.Media) emojis.Emoji {
	return emojis.Emoji{Name: name, URI: s.emojiURI(name), OriginalURL: media.PublicURL, PublicURL: media.PublicURL, MediaType: media.ContentType}
}

func (s *Service) emojiURI(name string) string {
	return s.publicURL + "/emojis/" + url.PathEscape(strings.Trim(strings.TrimSpace(name), ":"))
}

func remoteFilename(rawURL, fallback string) string {
	if parsed, err := url.Parse(rawURL); err == nil {
		if name := path.Base(parsed.Path); name != "." && name != "/" && name != "" {
			return name
		}
	}
	return fallback
}
