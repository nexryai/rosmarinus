package resolver

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	apnotes "github.com/nexryai/rosmarinus/internal/activitypub/notes"
	appolls "github.com/nexryai/rosmarinus/internal/activitypub/polls"
	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/idgen"
	"github.com/nexryai/rosmarinus/internal/mfm"
)

type Fetcher interface {
	FetchObject(context.Context, string, *actors.Actor) (map[string]any, error)
}

type WebFinger interface {
	ResolveActor(context.Context, string) (string, error)
}

type FederationPolicy interface {
	IsFederationHostBlocked(string) bool
	IsSelfFederationURL(string) bool
}

type ObjectLocker interface {
	Acquire(context.Context, string) (func(context.Context) error, bool, error)
}

type MediaScheduler interface {
	ScheduleMedia(context.Context, string) error
}

type Resolver struct {
	repo      actors.Repository
	fetcher   Fetcher
	signer    *actors.Actor
	webFinger WebFinger
	policy    FederationPolicy
	notes     domainnotes.Repository
	emojis    emojis.Repository
	polls     polls.Repository
	locker    ObjectLocker
	media     MediaScheduler
}

const remoteActorRefreshInterval = 24 * time.Hour

const noteResolutionLimit = 256

const (
	maxActorSummaryLength  = 2048
	maxActorLocationLength = 128
	maxActorURLLength      = 512
)

var actorBirthdayPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

func (r *Resolver) SetFederationPolicy(policy FederationPolicy) {
	r.policy = policy
}

func (r *Resolver) SetNoteRepository(notes domainnotes.Repository) {
	r.notes = notes
}

func (r *Resolver) SetEmojiRepository(repository emojis.Repository) {
	r.emojis = repository
}

func (r *Resolver) SetPollRepository(repository polls.Repository) {
	r.polls = repository
}

func (r *Resolver) SetObjectLocker(locker ObjectLocker) {
	r.locker = locker
}

func (r *Resolver) SetMediaScheduler(scheduler MediaScheduler) {
	r.media = scheduler
}

func (r *Resolver) SetWebFinger(webFinger WebFinger) {
	r.webFinger = webFinger
}

func New(repo actors.Repository, fetcher Fetcher, signer *actors.Actor) *Resolver {
	return &Resolver{repo: repo, fetcher: fetcher, signer: signer}
}

func NewWithWebFinger(repo actors.Repository, fetcher Fetcher, signer *actors.Actor, webFinger WebFinger) *Resolver {
	return &Resolver{repo: repo, fetcher: fetcher, signer: signer, webFinger: webFinger}
}

func (r *Resolver) ResolveActorHandle(ctx context.Context, handle string) (*actors.Actor, error) {
	if r.webFinger == nil {
		return nil, fmt.Errorf("webfinger resolver is not configured")
	}
	uri, err := r.webFinger.ResolveActor(ctx, handle)
	if err != nil {
		return nil, fmt.Errorf("resolve actor handle: %w", err)
	}
	return r.ResolveActor(ctx, uri)
}

func (r *Resolver) ResolveActor(ctx context.Context, uri string) (*actors.Actor, error) {
	if uri == "" {
		return nil, fmt.Errorf("actor uri is required")
	}
	host, err := resolvableHostOf(uri)
	if err != nil {
		return nil, err
	}
	if r.policy != nil && r.policy.IsFederationHostBlocked(host) {
		return nil, fmt.Errorf("actor host is blocked: %s", host)
	}
	existing, err := r.repo.FindAnyByURI(ctx, uri)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.IsSuspended {
		return nil, fmt.Errorf("actor is suspended: %s", uri)
	}
	if existing != nil && (existing.Host == nil || (!existing.LastFetchedAt.IsZero() && time.Since(existing.LastFetchedAt) < remoteActorRefreshInterval)) {
		return existing, nil
	}
	if r.policy != nil && r.policy.IsSelfFederationURL(uri) {
		return nil, fmt.Errorf("local actor not found: %s", uri)
	}
	if r.fetcher == nil {
		if existing != nil {
			return existing, nil
		}
		return nil, fmt.Errorf("actor fetcher is not configured")
	}
	unlock, err := r.acquireObjectLock(ctx, uri)
	if err != nil {
		if existing != nil {
			return existing, nil
		}
		return nil, err
	}
	if unlock != nil {
		defer func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = unlock(unlockCtx)
		}()
		latest, err := r.repo.FindAnyByURI(ctx, uri)
		if err != nil {
			return nil, err
		}
		if actorIsFresh(latest) {
			return latest, nil
		}
		if latest != nil {
			existing = latest
		}
	}
	object, err := r.fetcher.FetchObject(ctx, uri, r.signer)
	if err != nil {
		if existing != nil {
			return existing, nil
		}
		return nil, err
	}
	actor, err := ParseRemoteActor(object, uri)
	if err != nil {
		if existing != nil {
			return existing, nil
		}
		return nil, err
	}
	if existing != nil {
		actor.ID = existing.ID
	} else {
		actor.ID, err = idgen.NewUniqueObjectID(ctx, func(ctx context.Context, id string) (bool, error) {
			found, findErr := r.repo.FindAnyByID(ctx, id)
			return found != nil, findErr
		})
		if err != nil {
			return nil, fmt.Errorf("generate remote actor id: %w", err)
		}
	}
	_ = r.upsertRemoteEmojis(ctx, actor.Host, apnotes.ExtractEmojis(object["tag"]))
	if existing != nil {
		actor.FeaturedNoteIDs = existing.FeaturedNoteIDs
	}
	stored, err := r.repo.UpsertRemoteActor(ctx, actor)
	if err != nil {
		return nil, err
	}
	r.scheduleActorMedia(ctx, stored)
	featuredIDs, err := r.resolveFeaturedNotes(ctx, actor.FeaturedURI)
	if err != nil {
		return stored, nil
	}
	actor.FeaturedNoteIDs = featuredIDs
	return r.repo.UpsertRemoteActor(ctx, actor)
}

func actorIsFresh(actor *actors.Actor) bool {
	return actor != nil && (actor.Host == nil || (!actor.LastFetchedAt.IsZero() && time.Since(actor.LastFetchedAt) < remoteActorRefreshInterval))
}

func (r *Resolver) resolveFeaturedNotes(ctx context.Context, collectionURI string) ([]string, error) {
	if collectionURI == "" || r.notes == nil {
		return nil, nil
	}
	collection, err := r.fetcher.FetchObject(ctx, collectionURI, r.signer)
	if err != nil {
		return nil, err
	}
	if !aptypes.IsCollectionOrOrderedCollection(collection) {
		return nil, fmt.Errorf("featured object is not a collection")
	}
	items := collection["items"]
	if aptypes.IsOrderedCollection(collection) {
		items = collection["orderedItems"]
	}
	values := aptypes.ToArray(items)
	if len(values) > 5 {
		values = values[:5]
	}
	ids := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		uri, err := aptypes.GetAPID(value)
		if err != nil {
			return nil, err
		}
		var note *domainnotes.Note
		// A collection may inline objects it serves from its own origin. An
		// off-origin object is only a claim and must be fetched canonically so
		// the collection owner cannot substitute another server's Note body.
		if embedded, ok := value.(map[string]any); ok && sameOrigin(uri, collectionURI) {
			note, err = r.resolveNoteValue(ctx, uri, embedded, &noteResolution{history: map[string]struct{}{}})
		} else {
			note, err = r.ResolveNote(ctx, uri)
		}
		if err != nil {
			return nil, err
		}
		if note == nil {
			continue
		}
		if _, ok := seen[note.ID]; ok {
			continue
		}
		seen[note.ID] = struct{}{}
		ids = append(ids, note.ID)
	}
	return ids, nil
}

func (r *Resolver) ResolveNote(ctx context.Context, uri string) (*domainnotes.Note, error) {
	return r.resolveNote(ctx, uri, &noteResolution{history: map[string]struct{}{}})
}

func (r *Resolver) ResolveNoteLinks(ctx context.Context, rootURI, replyURI string, quoteURIs ...string) (*domainnotes.Note, *domainnotes.Note, error) {
	resolution := &noteResolution{history: map[string]struct{}{rootURI: {}}}
	reply, err := r.resolveNote(ctx, replyURI, resolution)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve reply: %w", err)
	}
	quote, err := r.resolveQuoteCandidates(ctx, quoteURIs, resolution)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve quote: %w", err)
	}
	return reply, quote, nil
}

type noteResolution struct {
	history map[string]struct{}
}

type permanentNoteResolutionError struct {
	err error
}

func (e *permanentNoteResolutionError) Error() string {
	return e.err.Error()
}

func (e *permanentNoteResolutionError) Unwrap() error {
	return e.err
}

func (r *Resolver) resolveNote(ctx context.Context, uri string, resolution *noteResolution) (*domainnotes.Note, error) {
	return r.resolveNoteValue(ctx, uri, nil, resolution)
}

func (r *Resolver) resolveNoteValue(ctx context.Context, uri string, embedded map[string]any, resolution *noteResolution) (*domainnotes.Note, error) {
	if uri == "" {
		return nil, nil
	}
	host, err := resolvableHostOf(uri)
	if err != nil {
		return nil, &permanentNoteResolutionError{err: err}
	}
	if r.policy != nil && r.policy.IsFederationHostBlocked(host) {
		return nil, &permanentNoteResolutionError{err: fmt.Errorf("note host is blocked: %s", host)}
	}
	if r.notes == nil {
		return nil, fmt.Errorf("note repository is not configured")
	}
	existing, err := r.notes.FindByURI(ctx, uri)
	if err != nil || existing != nil {
		return existing, err
	}
	if _, ok := resolution.history[uri]; ok {
		return nil, fmt.Errorf("cannot resolve already resolved note: %s", uri)
	}
	if len(resolution.history) >= noteResolutionLimit {
		return nil, fmt.Errorf("note resolution limit reached")
	}
	resolution.history[uri] = struct{}{}
	if r.policy != nil && r.policy.IsSelfFederationURL(uri) {
		return nil, &permanentNoteResolutionError{err: fmt.Errorf("local note not found: %s", uri)}
	}
	if r.fetcher == nil {
		return nil, fmt.Errorf("note fetcher is not configured")
	}

	unlock, err := r.acquireObjectLock(ctx, uri)
	if err != nil {
		return nil, err
	}
	if unlock != nil {
		defer func() {
			unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = unlock(unlockCtx)
		}()
		if existing, err := r.notes.FindByURI(ctx, uri); err != nil || existing != nil {
			return existing, err
		}
	}

	object := embedded
	if object == nil {
		object, err = r.fetcher.FetchObject(ctx, uri, r.signer)
		if err != nil {
			return nil, err
		}
	}
	if !aptypes.IsPost(object) {
		return nil, &permanentNoteResolutionError{err: fmt.Errorf("resolved object is not a post: %v", object["type"])}
	}
	parsed, err := apnotes.ParseRemoteNote(object, uri)
	if err != nil {
		return nil, &permanentNoteResolutionError{err: err}
	}
	author, err := r.ResolveActor(ctx, parsed.AttributedTo)
	if err != nil {
		return nil, fmt.Errorf("resolve note author: %w", err)
	}
	reply, quote, err := r.resolveNoteLinks(ctx, parsed, resolution)
	if err != nil {
		return nil, err
	}
	note := domainnotes.Note{
		URI: parsed.URI, AttributedTo: parsed.AttributedTo, AuthorID: author.ID,
		Text: parsed.Text, ContentWarning: parsed.ContentWarning, Sensitive: parsed.Sensitive,
		InReplyToURI: parsed.InReplyToURI, QuoteURI: resolvedQuoteURI(parsed.QuoteURI, quote),
		Visibility: domainnotes.Visibility(parsed.Visibility), MentionURIs: parsed.MentionURIs,
		VisibleUserURIs: parsed.VisibleUserURIs,
		Hashtags:        parsed.Hashtags, Emojis: parsed.Emojis, Attachments: parsed.Attachments,
		Raw: object, CreatedAt: time.Now().UTC(), PublishedAt: activityPublishedAt(object),
	}
	if reply != nil {
		note.ReplyID = reply.ID
	}
	if quote != nil {
		note.QuoteID = quote.ID
	}
	_ = r.upsertRemoteEmojis(ctx, author.Host, parsed.Emojis)
	stored, err := r.notes.UpsertRemoteNote(ctx, note)
	if err != nil {
		return nil, err
	}
	r.scheduleNoteMedia(ctx, stored)
	if r.polls != nil && aptypes.IsType(object, "Question") {
		poll, parseErr := appolls.ParseQuestion(object)
		if parseErr == nil {
			poll.NoteID = stored.ID
			poll.AuthorID = author.ID
			poll.AuthorHost = author.Host
			if _, err := r.polls.UpsertRemote(ctx, *poll); err != nil {
				return nil, err
			}
		}
	}
	return stored, nil
}

func sameOrigin(left, right string) bool {
	leftURL, leftErr := url.Parse(left)
	rightURL, rightErr := url.Parse(right)
	return leftErr == nil && rightErr == nil &&
		strings.EqualFold(leftURL.Scheme, rightURL.Scheme) &&
		strings.EqualFold(leftURL.Host, rightURL.Host)
}

func (r *Resolver) scheduleActorMedia(ctx context.Context, actor *actors.Actor) {
	if r.media == nil || actor == nil {
		return
	}
	for _, rawURL := range []string{actor.AvatarURL, actor.BannerURL} {
		if strings.TrimSpace(rawURL) != "" {
			_ = r.media.ScheduleMedia(ctx, rawURL)
		}
	}
}

func (r *Resolver) scheduleNoteMedia(ctx context.Context, note *domainnotes.Note) {
	if r.media == nil || note == nil {
		return
	}
	for _, attachment := range note.Attachments {
		if strings.TrimSpace(attachment.URL) != "" {
			_ = r.media.ScheduleMedia(ctx, attachment.URL)
		}
	}
	for _, emoji := range note.Emojis {
		if strings.TrimSpace(emoji.IconURL) != "" {
			_ = r.media.ScheduleMedia(ctx, emoji.IconURL)
		}
	}
}

func (r *Resolver) upsertRemoteEmojis(ctx context.Context, host *string, values []domainnotes.Emoji) error {
	if r.emojis == nil || host == nil {
		return nil
	}
	for _, value := range values {
		if _, err := r.emojis.UpsertRemote(ctx, emojis.Emoji{
			Host: *host, Name: value.Name, URI: value.URI,
			OriginalURL: value.IconURL, MediaType: value.MediaType,
			RemoteUpdatedAt: value.UpdatedAt,
		}); err != nil {
			return err
		}
		if r.media != nil && strings.TrimSpace(value.IconURL) != "" {
			_ = r.media.ScheduleMedia(ctx, value.IconURL)
		}
	}
	return nil
}

func (r *Resolver) resolveNoteLinks(ctx context.Context, parsed *apnotes.Note, resolution *noteResolution) (*domainnotes.Note, *domainnotes.Note, error) {
	reply, err := r.resolveNote(ctx, parsed.InReplyToURI, resolution)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve reply: %w", err)
	}
	quote, err := r.resolveQuoteCandidates(ctx, parsed.QuoteURIs, resolution)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve quote: %w", err)
	}
	return reply, quote, nil
}

func (r *Resolver) resolveQuoteCandidates(ctx context.Context, uris []string, resolution *noteResolution) (*domainnotes.Note, error) {
	seen := make(map[string]struct{}, len(uris))
	var temporaryError error
	for _, uri := range uris {
		if uri == "" {
			continue
		}
		if _, exists := seen[uri]; exists {
			continue
		}
		seen[uri] = struct{}{}
		quote, err := r.resolveNote(ctx, uri, resolution)
		if err == nil && quote != nil {
			return quote, nil
		}
		if err != nil && !isPermanentNoteResolutionError(err) && temporaryError == nil {
			temporaryError = err
		}
	}
	return nil, temporaryError
}

func resolvedQuoteURI(sourceURI string, quote *domainnotes.Note) string {
	if quote != nil {
		return quote.URI
	}
	return sourceURI
}

func isPermanentNoteResolutionError(err error) bool {
	var permanent *permanentNoteResolutionError
	if errors.As(err, &permanent) {
		return true
	}
	var status interface{ HTTPStatusCode() int }
	if !errors.As(err, &status) {
		return false
	}
	code := status.HTTPStatusCode()
	return code >= 400 && code < 500 && code != 408 && code != 429
}

func (r *Resolver) acquireObjectLock(ctx context.Context, uri string) (func(context.Context) error, error) {
	if r.locker == nil {
		return nil, nil
	}
	lockName := fmt.Sprintf("object:%x", sha256.Sum256([]byte(uri)))
	for attempt := 0; attempt < 50; attempt++ {
		unlock, acquired, err := r.locker.Acquire(ctx, lockName)
		if err != nil {
			return nil, fmt.Errorf("acquire AP object lock: %w", err)
		}
		if acquired {
			return unlock, nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("timed out acquiring AP object lock")
}

func activityPublishedAt(object map[string]any) *time.Time {
	value, _ := object["published"].(string)
	published, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil
	}
	published = published.UTC()
	return &published
}

func resolvableHostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", fmt.Errorf("invalid resolvable url: %s", raw)
	}
	return strings.ToLower(u.Hostname()), nil
}

func ParseRemoteActor(object map[string]any, uri string) (actors.Actor, error) {
	if object == nil {
		return actors.Actor{}, fmt.Errorf("invalid actor: object is null")
	}
	if !aptypes.IsActor(object) {
		return actors.Actor{}, fmt.Errorf("invalid actor type")
	}
	id, err := aptypes.GetAPID(object)
	if err != nil {
		return actors.Actor{}, fmt.Errorf("invalid actor id: %w", err)
	}
	expectAuthority, err := aptypes.Authority(uri)
	if err != nil {
		return actors.Actor{}, err
	}
	idAuthority, err := aptypes.Authority(id)
	if err != nil {
		return actors.Actor{}, fmt.Errorf("invalid actor id host: %w", err)
	}
	if idAuthority != expectAuthority {
		return actors.Actor{}, fmt.Errorf("invalid actor: id has different host")
	}
	inbox, err := requiredSameAuthorityID(object["inbox"], expectAuthority, "inbox")
	if err != nil {
		return actors.Actor{}, err
	}
	if _, err := optionalSameAuthorityID(object["outbox"], expectAuthority, "outbox"); err != nil {
		return actors.Actor{}, err
	}
	sharedInbox := optionalSharedInbox(object, expectAuthority)
	followersURI, err := optionalSameAuthorityID(object["followers"], expectAuthority, "followers")
	if err != nil {
		return actors.Actor{}, err
	}
	followingURI, err := optionalSameAuthorityID(object["following"], expectAuthority, "following")
	if err != nil {
		return actors.Actor{}, err
	}
	featuredURI := optionalAPID(object["featured"])
	movedToURI := optionalAPID(object["movedTo"])
	alsoKnownAs := aptypes.GetAPIDs(object["alsoKnownAs"])
	username, ok := object["preferredUsername"].(string)
	if !ok || !validRemoteUsername(username) {
		return actors.Actor{}, fmt.Errorf("invalid actor: wrong username")
	}
	name, nameOK := object["name"].(string)
	if object["name"] != nil && !nameOK {
		return actors.Actor{}, fmt.Errorf("invalid actor: wrong name")
	}
	name = truncateRunes(name, 128)
	summary, err := actorSummary(object)
	if err != nil {
		return actors.Actor{}, err
	}
	profileURL, err := optionalHTTPSHref(object["url"], maxActorURLLength)
	if err != nil {
		return actors.Actor{}, fmt.Errorf("invalid actor url: %w", err)
	}
	avatarURL, _ := optionalHTTPSHref(object["icon"], maxActorURLLength)
	bannerURL, _ := optionalHTTPSHref(object["image"], maxActorURLLength)
	profileFields := actorProfileFields(object["attachment"])
	tags := actorHashtags(object["tag"])
	emojiNames := actorEmojiNames(object["tag"])
	birthday := actorBirthday(object["vcard:bday"])
	location, _ := object["vcard:Address"].(string)
	location = truncateRunes(location, maxActorLocationLength)
	publicKeyID, publicKeyPEM, err := publicKey(object, expectAuthority)
	if err != nil {
		return actors.Actor{}, err
	}
	kind := actorType(object)
	host, err := resolvableHostOf(uri)
	if err != nil {
		return actors.Actor{}, err
	}
	return actors.Actor{
		Username:       username,
		UsernameLower:  strings.ToLower(username),
		Name:           name,
		Summary:        summary,
		URL:            profileURL,
		ProfileFields:  profileFields,
		Birthday:       birthday,
		Location:       location,
		AvatarURL:      avatarURL,
		BannerURL:      bannerURL,
		Tags:           tags,
		EmojiNames:     emojiNames,
		IsBot:          kind == "Service" || kind == "Application",
		IsCat:          boolValue(object["isCat"]),
		IsLocked:       boolValue(object["manuallyApprovesFollowers"]),
		IsDiscoverable: boolValue(object["discoverable"]),
		Type:           kind,
		Host:           &host,
		URI:            id,
		Inbox:          inbox,
		SharedInbox:    sharedInbox,
		FollowersURI:   followersURI,
		FollowingURI:   followingURI,
		FeaturedURI:    featuredURI,
		MovedToURI:     movedToURI,
		AlsoKnownAs:    alsoKnownAs,
		PublicKeyID:    publicKeyID,
		PublicKeyPEM:   publicKeyPEM,
		IsSuspended:    false,
	}, nil
}

func actorSummary(object map[string]any) (string, error) {
	if summary, ok := object["_misskey_summary"].(string); ok && summary != "" {
		return truncateRunes(summary, maxActorSummaryLength), nil
	}
	value, exists := object["summary"]
	if !exists || value == nil || value == "" {
		return "", nil
	}
	summary, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("invalid actor: wrong summary")
	}
	hashtags := actorHashtags(object["tag"])
	for i := range hashtags {
		hashtags[i] = "#" + hashtags[i]
	}
	converted, err := mfm.FromHTML(truncateRunes(summary, maxActorSummaryLength), hashtags)
	if err != nil {
		return "", fmt.Errorf("invalid actor summary: %w", err)
	}
	return converted, nil
}

func actorProfileFields(value any) []actors.ProfileField {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	fields := make([]actors.ProfileField, 0, len(items))
	for _, item := range items {
		property, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(property, "PropertyValue") {
			continue
		}
		name, nameOK := property["name"].(string)
		value, valueOK := property["value"].(string)
		if !nameOK || !valueOK {
			continue
		}
		converted, err := mfm.FromHTML(value, nil)
		if err != nil {
			continue
		}
		fields = append(fields, actors.ProfileField{Name: truncateRunes(name, 128), Value: truncateRunes(converted, 2048)})
	}
	return fields
}

func actorHashtags(value any) []string {
	result := make([]string, 0)
	seen := map[string]struct{}{}
	for _, item := range aptypes.ToArray(value) {
		tag, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(tag, "Hashtag") {
			continue
		}
		name, _ := tag["name"].(string)
		name = strings.TrimPrefix(strings.TrimSpace(name), "#")
		name = strings.ToLower(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
		if len(result) == 32 {
			break
		}
	}
	return result
}

func actorEmojiNames(value any) []string {
	result := make([]string, 0)
	seen := map[string]struct{}{}
	for _, item := range aptypes.ToArray(value) {
		tag, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(tag, "Emoji") {
			continue
		}
		name, _ := tag["name"].(string)
		name = strings.Trim(strings.TrimSpace(name), ":")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func actorBirthday(value any) string {
	birthday, _ := value.(string)
	return actorBirthdayPattern.FindString(birthday)
}

func optionalHTTPSHref(value any, maxLength int) (string, error) {
	for _, item := range aptypes.ToArray(value) {
		var href string
		switch link := item.(type) {
		case string:
			href = link
		case map[string]any:
			href, _ = link["url"].(string)
			if href == "" {
				href, _ = link["href"].(string)
			}
		}
		if href == "" {
			continue
		}
		parsed, err := url.Parse(href)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return "", fmt.Errorf("unexpected schema: %s", href)
		}
		if utf8.RuneCountInString(href) > maxLength {
			return "", fmt.Errorf("url exceeds %d characters", maxLength)
		}
		return href, nil
	}
	return "", nil
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func truncateRunes(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}

func requiredSameAuthorityID(value any, expectAuthority, field string) (string, error) {
	id, err := aptypes.GetAPID(value)
	if err != nil {
		return "", fmt.Errorf("invalid actor: wrong %s", field)
	}
	authority, err := aptypes.Authority(id)
	if err != nil || authority != expectAuthority {
		return "", fmt.Errorf("invalid actor: wrong %s", field)
	}
	return id, nil
}

func optionalSharedInbox(object map[string]any, expectAuthority string) string {
	value := object["sharedInbox"]
	if value == nil {
		if endpoints, ok := object["endpoints"].(map[string]any); ok {
			value = endpoints["sharedInbox"]
		}
	}
	if value == nil {
		return ""
	}
	sharedInbox, err := requiredSameAuthorityID(value, expectAuthority, "shared inbox")
	if err != nil {
		// A shared inbox is only an optimization. Falling back to the validated
		// individual inbox preserves federation without trusting a foreign host.
		return ""
	}
	return sharedInbox
}

func optionalSameAuthorityID(value any, expectAuthority, field string) (string, error) {
	if value == nil {
		return "", nil
	}
	return requiredSameAuthorityID(value, expectAuthority, field)
}

func optionalAPID(value any) string {
	if value == nil {
		return ""
	}
	id, err := aptypes.GetAPID(value)
	if err != nil {
		return ""
	}
	return id
}

func publicKey(object map[string]any, expectAuthority string) (string, string, error) {
	value, ok := object["publicKey"].(map[string]any)
	if !ok || value == nil {
		return "", "", nil
	}
	id, ok := value["id"].(string)
	if !ok || id == "" {
		return "", "", fmt.Errorf("invalid actor: publicKey.id is not a string")
	}
	authority, err := aptypes.Authority(id)
	if err != nil || authority != expectAuthority {
		return "", "", fmt.Errorf("invalid actor: publicKey.id has different host")
	}
	pem, ok := value["publicKeyPem"].(string)
	if !ok || pem == "" {
		return "", "", fmt.Errorf("invalid actor: publicKey.publicKeyPem is not a string")
	}
	return id, pem, nil
}

func actorType(object map[string]any) string {
	typ, err := aptypes.GetAPType(object)
	if err != nil {
		return "Person"
	}
	return typ
}

func validRemoteUsername(username string) bool {
	if username == "" || len(username) > 128 {
		return false
	}
	for i, r := range username {
		ok := r == '_' || r == '-' || r == '.' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if !ok {
			return false
		}
		if (i == 0 || i == len(username)-1) && (r == '-' || r == '.') {
			return false
		}
	}
	return true
}
