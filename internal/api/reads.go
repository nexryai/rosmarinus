package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/readmodel"
)

func (h *Handler) timeline(w http.ResponseWriter, r *http.Request, accountID, kind string) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	actorID, ok := h.requireOwnedActorQuery(w, r, accountID)
	if !ok {
		return
	}
	limit, after, ok := h.readPage(w, r)
	if !ok {
		return
	}
	if h.reader == nil {
		h.internalError(w, r, fmt.Errorf("read service is not configured"))
		return
	}
	var items []readmodel.Note
	var err error
	switch kind {
	case "public":
		items, err = h.reader.ListPublicTimeline(r.Context(), actorID, after, limit)
	case "home":
		items, err = h.reader.ListHomeTimeline(r.Context(), actorID, after, limit)
	default:
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if err != nil {
		h.internalError(w, r, fmt.Errorf("list %s timeline: %w", kind, err))
		return
	}
	h.writeNotePage(w, items, limit)
}

func (h *Handler) noteResource(w http.ResponseWriter, r *http.Request, accountID string, segments []string) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	actorID, ok := h.requireOwnedActorQuery(w, r, accountID)
	if !ok {
		return
	}
	if h.reader == nil {
		h.internalError(w, r, fmt.Errorf("read service is not configured"))
		return
	}
	if len(segments) == 1 {
		item, err := h.reader.FindVisibleNote(r.Context(), actorID, segments[0])
		if err != nil {
			h.internalError(w, r, fmt.Errorf("find visible note: %w", err))
			return
		}
		if item == nil {
			h.writeError(w, http.StatusNotFound, "note_not_found", "Note not found")
			return
		}
		h.writeJSON(w, http.StatusOK, map[string]any{"data": projectNote(*item)})
		return
	}
	if len(segments) == 2 && segments[1] == "thread" {
		limit, after, ok := h.readPage(w, r)
		if !ok {
			return
		}
		items, err := h.reader.ListVisibleThread(r.Context(), actorID, segments[0], after, limit)
		if err != nil {
			h.internalError(w, r, fmt.Errorf("list visible note thread: %w", err))
			return
		}
		h.writeNotePage(w, items, limit)
		return
	}
	h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
}

func (h *Handler) connections(w http.ResponseWriter, r *http.Request, accountID, actorID, kind string, segments []string) {
	if len(segments) != 0 {
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	if _, ok := h.authorizeActor(w, r, accountID, actorID, false); !ok {
		return
	}
	h.writeConnections(w, r, actorID, actorID, kind)
}

func (h *Handler) writeConnections(w http.ResponseWriter, r *http.Request, viewerActorID, actorID, kind string) {
	if h.reader == nil {
		h.internalError(w, r, fmt.Errorf("read service is not configured"))
		return
	}
	limit, err := pageLimit(r.URL.Query().Get("limit"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	items, err := h.reader.ListConnections(r.Context(), viewerActorID, actorID, kind, r.URL.Query().Get("after"), limit)
	if err != nil {
		h.internalError(w, r, fmt.Errorf("list %s: %w", kind, err))
		return
	}
	views := make([]connectionView, 0, len(items))
	for _, item := range items {
		views = append(views, connectionView{
			ID: item.Follow.ID, Status: string(item.Follow.Status), CreatedAt: item.Follow.CreatedAt,
			AcceptedAt: item.Follow.AcceptedAt, Actor: projectActor(item.Actor),
		})
	}
	next := ""
	if len(items) == limit {
		next = items[len(items)-1].Follow.ID
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"data": views, "next": next})
}

func (h *Handler) profileConnections(w http.ResponseWriter, r *http.Request, accountID, profileActorID, kind string) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	viewerActorID, ok := h.requireOwnedActorQuery(w, r, accountID)
	if !ok {
		return
	}
	if h.reader == nil {
		h.internalError(w, r, fmt.Errorf("read service is not configured"))
		return
	}
	profile, err := h.reader.FindProfile(r.Context(), viewerActorID, profileActorID)
	if err != nil {
		h.internalError(w, r, fmt.Errorf("authorize profile connections: %w", err))
		return
	}
	if profile == nil {
		h.writeError(w, http.StatusNotFound, "profile_not_found", "Profile not found")
		return
	}
	h.writeConnections(w, r, viewerActorID, profileActorID, kind)
}

func (h *Handler) profileNotes(w http.ResponseWriter, r *http.Request, accountID, profileActorID string) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	viewerActorID, ok := h.requireOwnedActorQuery(w, r, accountID)
	if !ok {
		return
	}
	if h.reader == nil {
		h.internalError(w, r, fmt.Errorf("read service is not configured"))
		return
	}
	profile, err := h.reader.FindProfile(r.Context(), viewerActorID, profileActorID)
	if err != nil {
		h.internalError(w, r, fmt.Errorf("authorize profile notes: %w", err))
		return
	}
	if profile == nil || profile.Actor == nil {
		h.writeError(w, http.StatusNotFound, "profile_not_found", "Profile not found")
		return
	}
	limit, after, ok := h.readPage(w, r)
	if !ok {
		return
	}
	items, err := h.reader.ListProfileNotes(r.Context(), viewerActorID, profileActorID, after, limit)
	if err != nil {
		h.internalError(w, r, fmt.Errorf("list profile notes: %w", err))
		return
	}
	h.writeNotePage(w, items, limit)
}

func (h *Handler) listNotifications(w http.ResponseWriter, r *http.Request, accountID, actorID string) {
	if _, ok := h.authorizeActor(w, r, accountID, actorID, false); !ok {
		return
	}
	h.writeNotifications(w, r, accountID, actorID)
}

func (h *Handler) accountNotifications(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	h.writeNotifications(w, r, accountID, "")
}

func (h *Handler) writeNotifications(w http.ResponseWriter, r *http.Request, accountID, actorID string) {
	if h.reader == nil {
		h.internalError(w, r, fmt.Errorf("read service is not configured"))
		return
	}
	limit, after, ok := h.readPage(w, r)
	if !ok {
		return
	}
	var unread *bool
	if raw := r.URL.Query().Get("unread"); raw != "" {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "invalid_unread", "unread must be true or false")
			return
		}
		unread = &value
	}
	items, err := h.reader.ListNotifications(r.Context(), accountID, actorID, after, limit, unread)
	if err != nil {
		h.internalError(w, r, fmt.Errorf("list notifications: %w", err))
		return
	}
	views := make([]notificationView, 0, len(items))
	for _, item := range items {
		view := notificationView{
			ID: item.Notification.ID, ActorID: item.Notification.RecipientActorID,
			Kind: item.Notification.Kind, NoteID: item.Notification.NoteID,
			CreatedAt: item.Notification.CreatedAt, IsRead: item.Notification.IsRead, ReadAt: item.Notification.ReadAt,
			Reaction: item.Reaction,
		}
		if actorID != "" && item.Source != nil {
			source := projectActor(item.Source)
			view.Source = &source
		}
		if actorID != "" && item.Note != nil {
			note := projectNote(*item.Note)
			view.Note = &note
		}
		if item.ReactionEmoji != nil {
			emoji := emojiView{Name: item.ReactionEmoji.Name, URL: item.ReactionEmoji.URL, MediaType: item.ReactionEmoji.MediaType}
			view.ReactionEmoji = &emoji
		}
		views = append(views, view)
	}
	next := ""
	if len(items) == limit {
		last := items[len(items)-1].Notification
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"data": views, "next": next})
}

func (h *Handler) profile(w http.ResponseWriter, r *http.Request, accountID, profileActorID string) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	viewerActorID, ok := h.requireOwnedActorQuery(w, r, accountID)
	if !ok {
		return
	}
	if h.reader == nil {
		h.internalError(w, r, fmt.Errorf("read service is not configured"))
		return
	}
	profile, err := h.reader.FindProfile(r.Context(), viewerActorID, profileActorID)
	if err != nil {
		h.internalError(w, r, fmt.Errorf("find profile: %w", err))
		return
	}
	if profile == nil || profile.Actor == nil {
		h.writeError(w, http.StatusNotFound, "profile_not_found", "Profile not found")
		return
	}
	h.writeProfile(w, profile)
}

func (h *Handler) resolveRemoteProfile(w http.ResponseWriter, r *http.Request, accountID, actorID string, segments []string) {
	if len(segments) != 1 || segments[0] != "resolve" {
		h.writeError(w, http.StatusNotFound, "not_found", "resource not found")
		return
	}
	if r.Method != http.MethodPost {
		h.methodNotAllowed(w, http.MethodPost)
		return
	}
	if _, ok := h.authorizeActor(w, r, accountID, actorID, false); !ok {
		return
	}
	var body struct {
		Target string `json:"target"`
	}
	if !h.decodeJSON(w, r, &body, false) {
		return
	}
	target := strings.TrimSpace(body.Target)
	if target == "" || len(target) > 2048 {
		h.writeError(w, http.StatusUnprocessableEntity, "invalid_target", "target must be a remote Actor handle or URL")
		return
	}
	if h.remoteProfiles == nil || h.reader == nil {
		h.internalError(w, r, fmt.Errorf("remote profile services are not configured"))
		return
	}
	remoteActor, err := h.remoteProfiles.ResolveRemoteActor(r.Context(), target)
	if err != nil || remoteActor == nil {
		if h.logger != nil {
			h.logger.Printf("api: remote profile resolution failed account_id=%s actor_id=%s err=%v", accountID, actorID, err)
		}
		h.writeError(w, http.StatusUnprocessableEntity, "profile_unresolvable", "Remote profile could not be resolved")
		return
	}
	profile, err := h.reader.FindProfile(r.Context(), actorID, remoteActor.ID)
	if err != nil {
		h.internalError(w, r, fmt.Errorf("project resolved remote profile: %w", err))
		return
	}
	if profile == nil || profile.Actor == nil {
		h.writeError(w, http.StatusNotFound, "profile_not_found", "Profile not found")
		return
	}
	h.writeProfile(w, profile)
}

func (h *Handler) writeProfile(w http.ResponseWriter, profile *readmodel.Profile) {
	pinnedNotes := make([]noteView, 0, len(profile.PinnedNotes))
	for _, note := range profile.PinnedNotes {
		pinnedNotes = append(pinnedNotes, projectNote(note))
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"data": profileView{
		Actor: projectActor(profile.Actor), FollowersCount: profile.FollowersCount, FollowingCount: profile.FollowingCount,
		FollowStatus: profile.FollowStatus, BlockedByViewer: profile.BlockedByViewer,
		MutedByViewer: profile.MutedByViewer, MuteExpiresAt: profile.MuteExpiresAt, PinnedNotes: pinnedNotes,
	}})
}

func (h *Handler) emojis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.methodNotAllowed(w, http.MethodGet)
		return
	}
	if h.reader == nil {
		h.internalError(w, r, fmt.Errorf("read service is not configured"))
		return
	}
	limit, err := pageLimit(r.URL.Query().Get("limit"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return
	}
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "local"
	}
	if scope != "local" && scope != "remote" {
		h.writeError(w, http.StatusBadRequest, "invalid_emoji_scope", "scope must be local or remote")
		return
	}
	after, err := decodeEmojiCursor(r.URL.Query().Get("after"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_cursor", "after is not a valid cursor")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	if len(query) > 100 || len(host) > 255 {
		h.writeError(w, http.StatusBadRequest, "invalid_emoji_filter", "emoji query or host is too long")
		return
	}
	items, err := h.reader.ListEmojis(r.Context(), readmodel.EmojiListQuery{Remote: scope == "remote", Query: query, Host: host, After: after, Limit: limit})
	if err != nil {
		h.internalError(w, r, fmt.Errorf("list local emoji: %w", err))
		return
	}
	views := make([]emojiView, 0, len(items))
	for _, item := range items {
		views = append(views, projectEmoji(item))
	}
	next := ""
	if len(items) == limit {
		last := items[len(items)-1]
		next = encodeEmojiCursor(readmodel.EmojiCursor{Host: last.Host, Name: last.Name, ID: last.ID})
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"data": views, "next": next})
}

func encodeEmojiCursor(cursor readmodel.EmojiCursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursor.Host + "\x00" + cursor.Name + "\x00" + cursor.ID))
}

func decodeEmojiCursor(raw string) (readmodel.EmojiCursor, error) {
	if raw == "" {
		return readmodel.EmojiCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return readmodel.EmojiCursor{}, err
	}
	parts := strings.Split(string(decoded), "\x00")
	if len(parts) != 3 || parts[2] == "" {
		return readmodel.EmojiCursor{}, fmt.Errorf("emoji cursor is incomplete")
	}
	return readmodel.EmojiCursor{Host: parts[0], Name: parts[1], ID: parts[2]}, nil
}

func (h *Handler) requireOwnedActorQuery(w http.ResponseWriter, r *http.Request, accountID string) (string, bool) {
	actorID := strings.TrimSpace(r.URL.Query().Get("actor_id"))
	if actorID == "" {
		h.writeError(w, http.StatusBadRequest, "actor_required", "actor_id is required")
		return "", false
	}
	actor, ok := h.authorizeActor(w, r, accountID, actorID, false)
	if !ok {
		return "", false
	}
	return actor.ID, true
}

func (h *Handler) readPage(w http.ResponseWriter, r *http.Request) (int, readmodel.Cursor, bool) {
	limit, err := pageLimit(r.URL.Query().Get("limit"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_limit", err.Error())
		return 0, readmodel.Cursor{}, false
	}
	after, err := decodeCursor(r.URL.Query().Get("after"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid_cursor", "after is not a valid cursor")
		return 0, readmodel.Cursor{}, false
	}
	return limit, after, true
}

func (h *Handler) writeNotePage(w http.ResponseWriter, items []readmodel.Note, limit int) {
	views := make([]noteView, 0, len(items))
	for _, item := range items {
		views = append(views, projectNote(item))
	}
	next := ""
	if len(items) == limit {
		last := items[len(items)-1].Note
		next = encodeCursor(last.CreatedAt, last.ID)
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"data": views, "next": next})
}

func encodeCursor(createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(createdAt.UTC().Format(time.RFC3339Nano) + "\x00" + id))
}

func decodeCursor(raw string) (readmodel.Cursor, error) {
	if raw == "" {
		return readmodel.Cursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return readmodel.Cursor{}, err
	}
	parts := strings.SplitN(string(decoded), "\x00", 2)
	if len(parts) != 2 || parts[1] == "" {
		return readmodel.Cursor{}, fmt.Errorf("cursor is incomplete")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return readmodel.Cursor{}, err
	}
	return readmodel.Cursor{CreatedAt: createdAt, ID: parts[1]}, nil
}

type noteView struct {
	ID             string                `json:"id"`
	URI            string                `json:"uri"`
	Text           string                `json:"text"`
	ContentWarning *string               `json:"content_warning,omitempty"`
	Sensitive      bool                  `json:"sensitive"`
	ReplyID        string                `json:"reply_id,omitempty"`
	QuoteID        string                `json:"quote_id,omitempty"`
	RenoteID       string                `json:"renote_id,omitempty"`
	Visibility     string                `json:"visibility"`
	MentionURIs    []string              `json:"mention_uris"`
	Hashtags       []string              `json:"hashtags"`
	Emojis         []noteEmojiView       `json:"emojis"`
	Attachments    []attachmentView      `json:"attachments"`
	CreatedAt      time.Time             `json:"created_at"`
	PublishedAt    *time.Time            `json:"published_at,omitempty"`
	RepliesCount   int                   `json:"replies_count"`
	Author         *actorView            `json:"author,omitempty"`
	Poll           *pollView             `json:"poll,omitempty"`
	Reactions      []reactionSummaryView `json:"reactions"`
	Reply          *noteReferenceView    `json:"reply,omitempty"`
	Quote          *noteReferenceView    `json:"quote,omitempty"`
	Renote         *noteReferenceView    `json:"renote,omitempty"`
}

type noteEmojiView struct {
	Name      string `json:"name"`
	IconURL   string `json:"url"`
	MediaType string `json:"media_type,omitempty"`
}

type attachmentView struct {
	Type      string `json:"type,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	URL       string `json:"url"`
	Name      string `json:"name,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	Sensitive bool   `json:"sensitive"`
}

type pollView struct {
	Choices   []pollChoiceView `json:"choices"`
	Multiple  bool             `json:"multiple"`
	ExpiresAt *time.Time       `json:"expires_at,omitempty"`
	Expired   bool             `json:"expired"`
}

type pollChoiceView struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
	Voted bool   `json:"voted"`
}

type reactionSummaryView struct {
	Reaction string     `json:"reaction"`
	Count    int        `json:"count"`
	Reacted  bool       `json:"reacted"`
	Emoji    *emojiView `json:"emoji,omitempty"`
}

type noteReferenceView struct {
	ID             string             `json:"id"`
	URI            string             `json:"uri"`
	Text           string             `json:"text"`
	ContentWarning *string            `json:"content_warning,omitempty"`
	Sensitive      bool               `json:"sensitive"`
	Visibility     string             `json:"visibility"`
	CreatedAt      time.Time          `json:"created_at"`
	RepliesCount   int                `json:"replies_count"`
	Author         *actorView         `json:"author,omitempty"`
	Emojis         []noteEmojiView    `json:"emojis"`
	Attachments    []attachmentView   `json:"attachments"`
	Reply          *noteReferenceView `json:"reply,omitempty"`
	Quote          *noteReferenceView `json:"quote,omitempty"`
}

type connectionView struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	Actor      actorView  `json:"actor"`
}

type notificationView struct {
	ID            string     `json:"id"`
	ActorID       string     `json:"actor_id"`
	Kind          string     `json:"kind"`
	NoteID        string     `json:"note_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	IsRead        bool       `json:"is_read"`
	ReadAt        *time.Time `json:"read_at,omitempty"`
	Source        *actorView `json:"source,omitempty"`
	Note          *noteView  `json:"note,omitempty"`
	Reaction      string     `json:"reaction,omitempty"`
	ReactionEmoji *emojiView `json:"reaction_emoji,omitempty"`
}

type profileView struct {
	Actor           actorView  `json:"actor"`
	FollowersCount  int        `json:"followers_count"`
	FollowingCount  int        `json:"following_count"`
	FollowStatus    string     `json:"follow_status,omitempty"`
	BlockedByViewer bool       `json:"blocked_by_viewer"`
	MutedByViewer   bool       `json:"muted_by_viewer"`
	MuteExpiresAt   *time.Time `json:"mute_expires_at,omitempty"`
	PinnedNotes     []noteView `json:"pinned_notes"`
}

type emojiView struct {
	ID          string     `json:"id,omitempty"`
	Host        string     `json:"host,omitempty"`
	Name        string     `json:"name"`
	URI         string     `json:"uri,omitempty"`
	URL         string     `json:"url"`
	OriginalURL string     `json:"original_url,omitempty"`
	MediaType   string     `json:"media_type,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

func projectNote(item readmodel.Note) noteView {
	view := noteView{
		ID: item.Note.ID, URI: item.Note.URI, Text: item.Note.Text,
		ContentWarning: item.Note.ContentWarning, Sensitive: item.Note.Sensitive,
		ReplyID: item.Note.ReplyID, QuoteID: item.Note.QuoteID, RenoteID: item.Note.RenoteID,
		Visibility: string(item.Note.Visibility), MentionURIs: nonNilStrings(item.Note.MentionURIs),
		Hashtags: nonNilStrings(item.Note.Hashtags), CreatedAt: item.Note.CreatedAt,
		PublishedAt: item.Note.PublishedAt, RepliesCount: item.RepliesCount, Emojis: make([]noteEmojiView, 0, len(item.Note.Emojis)),
		Attachments: make([]attachmentView, 0, len(item.Note.Attachments)),
		Reactions:   make([]reactionSummaryView, 0, len(item.Reactions)),
	}
	if item.Author != nil {
		author := projectActor(item.Author)
		view.Author = &author
	}
	for _, emoji := range item.Note.Emojis {
		view.Emojis = append(view.Emojis, noteEmojiView{Name: emoji.Name, IconURL: emoji.IconURL, MediaType: emoji.MediaType})
	}
	for _, attachment := range item.Note.Attachments {
		view.Attachments = append(view.Attachments, attachmentView{
			Type: attachment.Type, MediaType: attachment.MediaType, URL: attachment.URL,
			Name: attachment.Name, Width: attachment.Width, Height: attachment.Height, Sensitive: attachment.Sensitive,
		})
	}
	for _, reaction := range item.Reactions {
		projected := reactionSummaryView{Reaction: reaction.Reaction, Count: reaction.Count, Reacted: reaction.Reacted}
		if reaction.Emoji != nil {
			emoji := emojiView{Name: reaction.Emoji.Name, URL: reaction.Emoji.URL, MediaType: reaction.Emoji.MediaType}
			projected.Emoji = &emoji
		}
		view.Reactions = append(view.Reactions, projected)
	}
	if item.Poll != nil {
		view.Poll = projectPoll(item.Poll, item.MyVotes)
	}
	view.Reply = projectNoteReference(item.Reply)
	view.Quote = projectNoteReference(item.Quote)
	view.Renote = projectNoteReference(item.Renote)
	return view
}

func projectNoteReference(reference *readmodel.NoteReference) *noteReferenceView {
	if reference == nil {
		return nil
	}
	view := &noteReferenceView{
		ID: reference.Note.ID, URI: reference.Note.URI, Text: reference.Note.Text,
		ContentWarning: reference.Note.ContentWarning, Sensitive: reference.Note.Sensitive,
		Visibility: string(reference.Note.Visibility), CreatedAt: reference.Note.CreatedAt, RepliesCount: reference.RepliesCount,
		Emojis:      make([]noteEmojiView, 0, len(reference.Note.Emojis)),
		Attachments: make([]attachmentView, 0, len(reference.Note.Attachments)),
	}
	if reference.Author != nil {
		author := projectActor(reference.Author)
		view.Author = &author
	}
	for _, emoji := range reference.Note.Emojis {
		view.Emojis = append(view.Emojis, noteEmojiView{Name: emoji.Name, IconURL: emoji.IconURL, MediaType: emoji.MediaType})
	}
	for _, attachment := range reference.Note.Attachments {
		view.Attachments = append(view.Attachments, attachmentView{
			Type: attachment.Type, MediaType: attachment.MediaType, URL: attachment.URL,
			Name: attachment.Name, Width: attachment.Width, Height: attachment.Height, Sensitive: attachment.Sensitive,
		})
	}
	view.Reply = projectShallowNoteReference(reference.Reply)
	view.Quote = projectShallowNoteReference(reference.Quote)
	return view
}

func projectShallowNoteReference(reference *readmodel.NoteReference) *noteReferenceView {
	if reference == nil {
		return nil
	}
	view := projectNoteReference(&readmodel.NoteReference{Note: reference.Note, Author: reference.Author})
	return view
}

func projectPoll(poll *polls.Poll, myVotes []int) *pollView {
	view := &pollView{Multiple: poll.Multiple, ExpiresAt: poll.ExpiresAt, Choices: make([]pollChoiceView, 0, len(poll.Choices))}
	view.Expired = poll.ExpiresAt != nil && !time.Now().UTC().Before(*poll.ExpiresAt)
	for index, text := range poll.Choices {
		votes := 0
		if index < len(poll.Votes) {
			votes = poll.Votes[index]
		}
		voted := false
		for _, choice := range myVotes {
			if choice == index {
				voted = true
				break
			}
		}
		view.Choices = append(view.Choices, pollChoiceView{Index: index, Text: text, Votes: votes, Voted: voted})
	}
	return view
}

func projectEmoji(emoji emojis.Emoji) emojiView {
	url := emoji.PublicURL
	if url == "" {
		url = emoji.OriginalURL
	}
	view := emojiView{ID: emoji.ID, Host: emoji.Host, Name: emoji.Name, URI: emoji.URI, URL: url, OriginalURL: emoji.OriginalURL, MediaType: emoji.MediaType}
	if !emoji.CreatedAt.IsZero() {
		createdAt := emoji.CreatedAt
		view.CreatedAt = &createdAt
	}
	if !emoji.UpdatedAt.IsZero() {
		updatedAt := emoji.UpdatedAt
		view.UpdatedAt = &updatedAt
	}
	return view
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
