package main

// This command serves a fixed, in-memory dataset so Salvia can be developed
// without MongoDB, Redis, object storage, or federation. Mutations are
// intentionally ignored: every response is derived from the immutable data
// below and never changes between requests.

const (
	demoAccountID     = "dev-account"
	demoCSRFToken     = "dev-csrf-token"
	demoUsername      = "rosemary"
	demoDisplayName   = "Rosemary"
	demoSelectedActor = "dev-actor-1"
	demoCreatedAt     = "2026-09-01T12:00:00Z"
)

const placeholderImage = "data:image/svg+xml,%3Csvg%20xmlns='http://www.w3.org/2000/svg'%20width='640'%20height='360'%3E%3Crect%20width='100%25'%20height='100%25'%20fill='%23f5c542'/%3E%3Ctext%20x='50%25'%20y='50%25'%20font-size='32'%20text-anchor='middle'%20fill='%23333'%20dy='.35em'%3ESalvia%20dev%3C/text%3E%3C/svg%3E"

type profileField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type emoji struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	MediaType string `json:"media_type,omitempty"`
}

type managedEmoji struct {
	emoji
	ID          string `json:"id"`
	Host        string `json:"host"`
	URI         string `json:"uri"`
	OriginalURL string `json:"original_url"`
	CreatedAt   string `json:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

type actor struct {
	ID             string         `json:"id"`
	Username       string         `json:"username"`
	Name           string         `json:"name"`
	Summary        string         `json:"summary"`
	URL            string         `json:"url"`
	ProfileFields  []profileField `json:"profile_fields"`
	Birthday       string         `json:"birthday"`
	Location       string         `json:"location"`
	AvatarURL      string         `json:"avatar_url"`
	BannerURL      string         `json:"banner_url"`
	Tags           []string       `json:"tags"`
	EmojiNames     []string       `json:"emoji_names"`
	Emojis         []emoji        `json:"emojis"`
	IsBot          bool           `json:"is_bot"`
	IsCat          bool           `json:"is_cat"`
	IsLocked       bool           `json:"is_locked"`
	IsDiscoverable bool           `json:"is_discoverable"`
	Type           string         `json:"type"`
	URI            string         `json:"uri"`
	MovedToURI     string         `json:"moved_to_uri"`
	IsSuspended    bool           `json:"is_suspended"`
}

type attachment struct {
	Type         string `json:"type,omitempty"`
	MediaType    string `json:"media_type,omitempty"`
	URL          string `json:"url"`
	ThumbnailURL string `json:"thumbnail_url,omitempty"`
	Name         string `json:"name,omitempty"`
	Width        int    `json:"width,omitempty"`
	Height       int    `json:"height,omitempty"`
	Sensitive    bool   `json:"sensitive"`
}

type reaction struct {
	Reaction string `json:"reaction"`
	Count    int    `json:"count"`
	Reacted  bool   `json:"reacted"`
	Emoji    *emoji `json:"emoji,omitempty"`
}

type pollChoice struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
	Voted bool   `json:"voted"`
}

type poll struct {
	Choices   []pollChoice `json:"choices"`
	Multiple  bool         `json:"multiple"`
	ExpiresAt *string      `json:"expires_at"`
	Expired   bool         `json:"expired"`
}

type mentionActor struct {
	ID        string  `json:"id"`
	Username  string  `json:"username"`
	Name      string  `json:"name"`
	Host      string  `json:"host"`
	AvatarURL string  `json:"avatar_url"`
	URI       string  `json:"uri"`
	Emojis    []emoji `json:"emojis"`
}

type noteReference struct {
	ID             string         `json:"id"`
	URI            string         `json:"uri"`
	Text           string         `json:"text"`
	ContentWarning *string        `json:"content_warning"`
	Sensitive      bool           `json:"sensitive"`
	Visibility     string         `json:"visibility"`
	CreatedAt      string         `json:"created_at"`
	RepliesCount   int            `json:"replies_count"`
	Author         *actor         `json:"author,omitempty"`
	Mentions       []mentionActor `json:"mentions"`
	Emojis         []emoji        `json:"emojis"`
	Attachments    []attachment   `json:"attachments"`
	Reactions      []reaction     `json:"reactions"`
}

type note struct {
	ID             string         `json:"id"`
	URI            string         `json:"uri"`
	Text           string         `json:"text"`
	ContentWarning *string        `json:"content_warning"`
	Sensitive      bool           `json:"sensitive"`
	ReplyID        string         `json:"reply_id,omitempty"`
	QuoteID        string         `json:"quote_id,omitempty"`
	RenoteID       string         `json:"renote_id,omitempty"`
	Visibility     string         `json:"visibility"`
	MentionURIs    []string       `json:"mention_uris"`
	Mentions       []mentionActor `json:"mentions"`
	Hashtags       []string       `json:"hashtags"`
	Emojis         []emoji        `json:"emojis"`
	Attachments    []attachment   `json:"attachments"`
	CreatedAt      string         `json:"created_at"`
	PublishedAt    *string        `json:"published_at"`
	RepliesCount   int            `json:"replies_count"`
	Author         *actor         `json:"author,omitempty"`
	Poll           *poll          `json:"poll,omitempty"`
	Reactions      []reaction     `json:"reactions"`
	Reply          *noteReference `json:"reply,omitempty"`
	Quote          *noteReference `json:"quote,omitempty"`
	Renote         *noteReference `json:"renote,omitempty"`
}

type notification struct {
	ID            string  `json:"id"`
	ActorID       string  `json:"actor_id"`
	Kind          string  `json:"kind"`
	NoteID        string  `json:"note_id,omitempty"`
	CreatedAt     string  `json:"created_at"`
	IsRead        bool    `json:"is_read"`
	ReadAt        *string `json:"read_at"`
	Source        *actor  `json:"source,omitempty"`
	Note          *note   `json:"note,omitempty"`
	Reaction      string  `json:"reaction,omitempty"`
	ReactionEmoji *emoji  `json:"reaction_emoji,omitempty"`
}

type connection struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	CreatedAt  string  `json:"created_at"`
	AcceptedAt *string `json:"accepted_at"`
	Actor      actor   `json:"actor"`
}

type antenna struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Source          string     `json:"source"`
	Users           []string   `json:"users"`
	Keywords        [][]string `json:"keywords"`
	ExcludeKeywords [][]string `json:"exclude_keywords"`
	CaseSensitive   bool       `json:"case_sensitive"`
	LocalOnly       bool       `json:"local_only"`
	ExcludeBots     bool       `json:"exclude_bots"`
	WithReplies     bool       `json:"with_replies"`
	WithFile        bool       `json:"with_file"`
	CreatedAt       string     `json:"created_at"`
	UpdatedAt       string     `json:"updated_at"`
}

type profile struct {
	Actor           actor   `json:"actor"`
	FollowersCount  int     `json:"followers_count"`
	FollowingCount  int     `json:"following_count"`
	FollowStatus    string  `json:"follow_status"`
	BlockedByViewer bool    `json:"blocked_by_viewer"`
	MutedByViewer   bool    `json:"muted_by_viewer"`
	MuteExpiresAt   *string `json:"mute_expires_at"`
	PinnedNotes     []note  `json:"pinned_notes"`
}

type session struct {
	AccountID   string `json:"account_id"`
	CSRFToken   string `json:"csrf_token"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

type accountSettings struct {
	Theme           string `json:"theme"`
	ReduceMotion    bool   `json:"reduce_motion"`
	CompactMode     bool   `json:"compact_mode"`
	SelectedActorID string `json:"selected_actor_id,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type actorSettings struct {
	ActorID            string `json:"actor_id"`
	DefaultVisibility  string `json:"default_visibility"`
	ShowContentWarning bool   `json:"show_content_warning"`
	DisplayOrder       int    `json:"display_order"`
	Color              string `json:"color,omitempty"`
	Pinned             bool   `json:"pinned"`
	UpdatedAt          string `json:"updated_at,omitempty"`
}

type instanceInfo struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	URL         string `json:"url"`
	Version     string `json:"version"`
	PasskeyOnly bool   `json:"passkey_only"`
}

type demoData struct {
	session         session
	accountSettings accountSettings
	actors          []actor
	actorSettings   map[string]actorSettings
	notes           []note
	notesByID       map[string]*note
	notifications   []notification
	emojis          []managedEmoji
	connections     map[string][]connection
	antennas        []antenna
}

func buildDemoData() *demoData {
	rosemary := actor{
		ID: "dev-actor-1", Username: "rosemary", Name: "Rosemary",
		Summary: "Rosemary is the local development actor.\n\n**MFM** works here: :rosemary: #salvia",
		URL:     "https://salvia.dev/@rosemary",
		ProfileFields: []profileField{
			{Name: "Website", Value: "https://salvia.dev"},
			{Name: "Location", Value: "Local development"},
		},
		Location:       "Local development",
		Tags:           []string{"dev", "salvia"},
		EmojiNames:     []string{"rosemary"},
		Emojis:         []emoji{{Name: "rosemary", URL: placeholderImage, MediaType: "image/svg+xml"}},
		IsCat:          true,
		IsDiscoverable: true,
		Type:           "Person",
		URI:            "https://salvia.dev/users/rosemary",
	}
	thyme := actor{
		ID: "dev-actor-2", Username: "thyme", Name: "Thyme",
		Summary:        "A second local actor used to exercise multi-Actor workflows.",
		URL:            "https://salvia.dev/@thyme",
		ProfileFields:  []profileField{{Name: "Role", Value: "Test actor"}},
		IsDiscoverable: true,
		Type:           "Person",
		URI:            "https://salvia.dev/users/thyme",
	}
	mint := actor{
		ID: "dev-remote-1", Username: "mint", Name: "Mint",
		Summary:        "Remote actor from a fictional server.",
		URL:            "https://mint.example/@mint",
		IsDiscoverable: true,
		Type:           "Person",
		URI:            "https://mint.example/users/mint",
	}
	sage := actor{
		ID: "dev-remote-2", Username: "sage", Name: "Sage Bot",
		Summary:        "A remote bot account.",
		URL:            "https://sage.example/@sage",
		IsBot:          true,
		IsDiscoverable: true,
		Type:           "Service",
		URI:            "https://sage.example/users/sage",
	}

	cw := "Content warning demo"
	notes := []note{
		{
			ID: "dev-note-1", URI: "https://salvia.dev/notes/dev-note-1",
			Text:       "Welcome to the Salvia development server! This timeline is fixed dummy data.\n\nTry **bold**, `code`, and a custom emoji :rosemary:.\n\nThanks @thyme and @mint@mint.example for testing mentions.",
			Visibility: "public", MentionURIs: []string{thyme.URI, mint.URI}, Hashtags: []string{"salvia", "dev"},
			Mentions: []mentionActor{
				{ID: thyme.ID, Username: thyme.Username, Name: thyme.Name, Host: "", AvatarURL: placeholderImage, URI: thyme.URI, Emojis: []emoji{}},
				{ID: mint.ID, Username: mint.Username, Name: mint.Name, Host: "mint.example", AvatarURL: placeholderImage, URI: mint.URI, Emojis: []emoji{}},
			},
			CreatedAt: "2026-09-01T12:00:00Z", Author: &rosemary,
			Reactions: []reaction{{Reaction: "⭐", Count: 3, Reacted: true}, {Reaction: "👍", Count: 1, Reacted: false}},
		},
		{
			ID: "dev-note-2", URI: "https://mint.example/notes/dev-note-2",
			Text:       "Hello from a remote actor. This one has an attachment.",
			Visibility: "public", CreatedAt: "2026-09-01T12:05:00Z", Author: &mint,
			Attachments: []attachment{{Type: "Image", MediaType: "image/svg+xml", URL: placeholderImage, ThumbnailURL: placeholderImage, Width: 640, Height: 360, Sensitive: false}},
			Reactions:   []reaction{{Reaction: "🎉", Count: 2, Reacted: false}},
		},
		{
			ID: "dev-note-3", URI: "https://salvia.dev/notes/dev-note-3",
			Text:       "Which theme do you prefer?",
			Visibility: "public", CreatedAt: "2026-09-01T12:10:00Z", Author: &thyme,
			Poll: &poll{
				Choices: []pollChoice{
					{Index: 0, Text: "Yellow", Votes: 4, Voted: true},
					{Index: 1, Text: "Dark", Votes: 2, Voted: false},
					{Index: 2, Text: "System", Votes: 1, Voted: false},
				},
				ExpiresAt: nil, Expired: false,
			},
		},
		{
			ID: "dev-note-4", URI: "https://sage.example/notes/dev-note-4",
			Text:       "Replying to Mint's note from the dummy dataset.",
			Visibility: "home", CreatedAt: "2026-09-01T12:15:00Z", Author: &sage,
			ReplyID: "dev-note-2",
		},
		{
			ID: "dev-note-5", URI: "https://salvia.dev/notes/dev-note-5",
			Text:       "Quoting the remote note to exercise quote rendering.",
			Visibility: "public", CreatedAt: "2026-09-01T12:20:00Z", Author: &rosemary,
			QuoteID: "dev-note-2",
		},
		{
			ID: "dev-note-6", URI: "https://salvia.dev/notes/dev-note-6",
			Text:       "This note has a content warning.",
			Visibility: "public", CreatedAt: "2026-09-01T12:25:00Z", Author: &rosemary,
			ContentWarning: &cw, Sensitive: true,
		},
		{
			ID: "dev-note-7", URI: "https://mint.example/notes/dev-note-7",
			Text:       "A longer remote note to check wrapping and vertical rhythm. " + "Lorem ipsum dolor sit amet, consectetur adipiscing elit. " + "Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
			Visibility: "public", CreatedAt: "2026-09-01T12:30:00Z", Author: &mint,
		},
		{
			ID: "dev-note-8", URI: "https://salvia.dev/notes/dev-note-8",
			Text:       "",
			Visibility: "public", CreatedAt: "2026-09-01T12:35:00Z", Author: &thyme,
			RenoteID: "dev-note-2",
		},
	}

	byID := make(map[string]*note, len(notes))
	for i := range notes {
		byID[notes[i].ID] = &notes[i]
	}
	reference := func(id string) *noteReference {
		target, ok := byID[id]
		if !ok {
			return nil
		}
		return &noteReference{
			ID: target.ID, URI: target.URI, Text: target.Text, ContentWarning: target.ContentWarning,
			Sensitive: target.Sensitive, Visibility: target.Visibility, CreatedAt: target.CreatedAt,
			RepliesCount: target.RepliesCount, Author: target.Author, Mentions: target.Mentions, Emojis: target.Emojis,
			Attachments: target.Attachments, Reactions: target.Reactions,
		}
	}
	if target := byID["dev-note-4"]; target != nil {
		target.Reply = reference("dev-note-2")
	}
	if target := byID["dev-note-5"]; target != nil {
		target.Quote = reference("dev-note-2")
	}
	if target := byID["dev-note-8"]; target != nil {
		target.Renote = reference("dev-note-2")
	}
	byID["dev-note-2"].RepliesCount = 1

	readAt := "2026-09-01T13:00:00Z"
	notifications := []notification{
		{ID: "dev-notif-1", ActorID: demoSelectedActor, Kind: "reaction", NoteID: "dev-note-1", CreatedAt: "2026-09-01T12:40:00Z", IsRead: false, Source: &mint, Note: byID["dev-note-1"], Reaction: "⭐"},
		{ID: "dev-notif-2", ActorID: demoSelectedActor, Kind: "reply", NoteID: "dev-note-2", CreatedAt: "2026-09-01T12:41:00Z", IsRead: false, Source: &sage, Note: byID["dev-note-4"]},
		{ID: "dev-notif-3", ActorID: demoSelectedActor, Kind: "renote", NoteID: "dev-note-6", CreatedAt: "2026-09-01T12:42:00Z", IsRead: false, Source: &mint, Note: byID["dev-note-7"]},
		{ID: "dev-notif-4", ActorID: demoSelectedActor, Kind: "mention", NoteID: "dev-note-1", CreatedAt: "2026-09-01T12:43:00Z", IsRead: true, ReadAt: &readAt, Source: &mint, Note: byID["dev-note-1"]},
		{ID: "dev-notif-5", ActorID: demoSelectedActor, Kind: "followRequest", CreatedAt: "2026-09-01T12:44:00Z", IsRead: false, Source: &mint},
		{ID: "dev-notif-6", ActorID: demoSelectedActor, Kind: "pollEnded", NoteID: "dev-note-3", CreatedAt: "2026-09-01T12:45:00Z", IsRead: true, ReadAt: &readAt, Source: &thyme, Note: byID["dev-note-3"]},
	}

	accepted := "2026-08-30T09:00:00Z"
	connections := map[string][]connection{
		"dev-actor-1": {
			{ID: "dev-conn-1", Status: "accepted", CreatedAt: "2026-08-30T08:00:00Z", AcceptedAt: &accepted, Actor: mint},
			{ID: "dev-conn-2", Status: "accepted", CreatedAt: "2026-08-30T08:05:00Z", AcceptedAt: &accepted, Actor: sage},
		},
		"dev-actor-2": {
			{ID: "dev-conn-3", Status: "accepted", CreatedAt: "2026-08-30T08:10:00Z", AcceptedAt: &accepted, Actor: mint},
		},
	}

	antennas := []antenna{
		{
			ID: "dev-antenna-1", Name: "Development", Source: "all",
			Users: []string{}, Keywords: [][]string{{"dev"}, {"salvia"}}, ExcludeKeywords: [][]string{},
			CreatedAt: "2026-08-30T10:00:00Z", UpdatedAt: "2026-08-30T10:00:00Z",
		},
	}

	return &demoData{
		session:         session{AccountID: demoAccountID, CSRFToken: demoCSRFToken, Username: demoUsername, DisplayName: demoDisplayName},
		accountSettings: accountSettings{Theme: "yellow", ReduceMotion: false, CompactMode: false, SelectedActorID: demoSelectedActor, UpdatedAt: demoCreatedAt},
		actors:          []actor{rosemary, thyme},
		actorSettings: map[string]actorSettings{
			"dev-actor-1": {ActorID: "dev-actor-1", DefaultVisibility: "public", DisplayOrder: 0, Pinned: true, UpdatedAt: demoCreatedAt},
			"dev-actor-2": {ActorID: "dev-actor-2", DefaultVisibility: "home", DisplayOrder: 1, Pinned: false, UpdatedAt: demoCreatedAt},
		},
		notes:         notes,
		notesByID:     byID,
		notifications: notifications,
		emojis: []managedEmoji{
			{emoji: emoji{Name: "rosemary", URL: placeholderImage, MediaType: "image/svg+xml"}, ID: "dev-emoji-1", Host: "", URI: "https://salvia.dev/emojis/rosemary", OriginalURL: placeholderImage, CreatedAt: demoCreatedAt, UpdatedAt: demoCreatedAt},
		},
		connections: connections,
		antennas:    antennas,
	}
}

func (d *demoData) findActor(id string) *actor {
	for i := range d.actors {
		if d.actors[i].ID == id {
			return &d.actors[i]
		}
	}
	for i := range d.notes {
		if d.notes[i].Author != nil && d.notes[i].Author.ID == id {
			return d.notes[i].Author
		}
	}
	for _, list := range d.connections {
		for i := range list {
			if list[i].Actor.ID == id {
				actor := list[i].Actor
				return &actor
			}
		}
	}
	return nil
}

func (d *demoData) profileFor(id string) profile {
	target := d.findActor(id)
	if target == nil {
		target = &d.actors[0]
	}
	pinned := make([]note, 0, 2)
	for _, candidate := range d.notes {
		if candidate.Author != nil && candidate.Author.ID == target.ID {
			pinned = append(pinned, candidate)
		}
		if len(pinned) == 2 {
			break
		}
	}
	connections := d.connections[target.ID]
	return profile{
		Actor:          *target,
		FollowersCount: 12,
		FollowingCount: len(connections),
		FollowStatus:   "",
		PinnedNotes:    pinned,
	}
}
