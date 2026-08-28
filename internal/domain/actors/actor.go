package actors

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

type Actor struct {
	ID              string
	OwnerAccountID  string
	IsSystemActor   bool
	Username        string
	UsernameLower   string
	Name            string
	Summary         string
	URL             string
	ProfileFields   []ProfileField
	Birthday        string
	Location        string
	AvatarURL       string
	BannerURL       string
	Tags            []string
	EmojiNames      []string
	IsBot           bool
	IsCat           bool
	IsLocked        bool
	IsDiscoverable  bool
	Type            string
	Host            *string
	URI             string
	Inbox           string
	SharedInbox     string
	FollowersURI    string
	FollowingURI    string
	FeaturedURI     string
	FeaturedNoteIDs []string
	MovedToURI      string
	AlsoKnownAs     []string
	MovedAt         *time.Time
	LastFetchedAt   time.Time
	PublicKeyID     string
	PublicKeyPEM    string
	PrivateKeyPEM   string
	IsSuspended     bool
}

type ProfileField struct {
	Name  string
	Value string
}

// Optional records whether a patch field was supplied. A supplied null value
// has Set=true and Value=nil, while an omitted field has Set=false.
type Optional[T any] struct {
	Set   bool
	Value *T
}

func Present[T any](value T) Optional[T] {
	return Optional[T]{Set: true, Value: &value}
}

func Clear[T any]() Optional[T] {
	return Optional[T]{Set: true}
}

func (o Optional[T]) IsSet() bool {
	return o.Set || o.Value != nil
}

func (o Optional[T]) IsNull() bool {
	return o.IsSet() && o.Value == nil
}

func (o *Optional[T]) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.Value = nil
		return nil
	}
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

func (o Optional[T]) MarshalJSON() ([]byte, error) {
	if o.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}

type ActorPatch struct {
	Name           Optional[string]         `json:"name"`
	Summary        Optional[string]         `json:"summary"`
	URL            Optional[string]         `json:"url"`
	ProfileFields  Optional[[]ProfileField] `json:"profile_fields"`
	Birthday       Optional[string]         `json:"birthday"`
	Location       Optional[string]         `json:"location"`
	AvatarURL      Optional[string]         `json:"avatar_url"`
	BannerURL      Optional[string]         `json:"banner_url"`
	Tags           Optional[[]string]       `json:"tags"`
	EmojiNames     Optional[[]string]       `json:"emoji_names"`
	IsBot          Optional[bool]           `json:"is_bot"`
	IsCat          Optional[bool]           `json:"is_cat"`
	IsLocked       Optional[bool]           `json:"is_locked"`
	IsDiscoverable Optional[bool]           `json:"is_discoverable"`
}

func (p ActorPatch) MarshalJSON() ([]byte, error) {
	values := make(map[string]any)
	if p.Name.IsSet() {
		values["name"] = p.Name.Value
	}
	if p.Summary.IsSet() {
		values["summary"] = p.Summary.Value
	}
	if p.URL.IsSet() {
		values["url"] = p.URL.Value
	}
	if p.ProfileFields.IsSet() {
		values["profile_fields"] = p.ProfileFields.Value
	}
	if p.Birthday.IsSet() {
		values["birthday"] = p.Birthday.Value
	}
	if p.Location.IsSet() {
		values["location"] = p.Location.Value
	}
	if p.AvatarURL.IsSet() {
		values["avatar_url"] = p.AvatarURL.Value
	}
	if p.BannerURL.IsSet() {
		values["banner_url"] = p.BannerURL.Value
	}
	if p.Tags.IsSet() {
		values["tags"] = p.Tags.Value
	}
	if p.EmojiNames.IsSet() {
		values["emoji_names"] = p.EmojiNames.Value
	}
	if p.IsBot.IsSet() {
		values["is_bot"] = p.IsBot.Value
	}
	if p.IsCat.IsSet() {
		values["is_cat"] = p.IsCat.Value
	}
	if p.IsLocked.IsSet() {
		values["is_locked"] = p.IsLocked.Value
	}
	if p.IsDiscoverable.IsSet() {
		values["is_discoverable"] = p.IsDiscoverable.Value
	}
	return json.Marshal(values)
}

func (p ActorPatch) IsEmpty() bool {
	return !p.Name.IsSet() && !p.Summary.IsSet() && !p.URL.IsSet() && !p.ProfileFields.IsSet() &&
		!p.Birthday.IsSet() && !p.Location.IsSet() && !p.AvatarURL.IsSet() &&
		!p.BannerURL.IsSet() && !p.Tags.IsSet() && !p.EmojiNames.IsSet() &&
		!p.IsBot.IsSet() && !p.IsCat.IsSet() && !p.IsLocked.IsSet() &&
		!p.IsDiscoverable.IsSet()
}

// Apply is useful for non-Mongo implementations and mirrors the repository's
// atomic patch semantics without touching server-owned Actor fields.
func (p ActorPatch) Apply(actor Actor) Actor {
	if p.Name.IsSet() {
		if p.Name.Value == nil {
			actor.Name = ""
		} else {
			actor.Name = *p.Name.Value
		}
	}
	if p.Summary.IsSet() {
		if p.Summary.Value == nil {
			actor.Summary = ""
		} else {
			actor.Summary = *p.Summary.Value
		}
	}
	if p.URL.IsSet() {
		if p.URL.Value == nil {
			actor.URL = ""
		} else {
			actor.URL = *p.URL.Value
		}
	}
	if p.ProfileFields.IsSet() {
		if p.ProfileFields.Value == nil {
			actor.ProfileFields = nil
		} else {
			actor.ProfileFields = append([]ProfileField(nil), (*p.ProfileFields.Value)...)
		}
	}
	if p.Birthday.IsSet() {
		if p.Birthday.Value == nil {
			actor.Birthday = ""
		} else {
			actor.Birthday = *p.Birthday.Value
		}
	}
	if p.Location.IsSet() {
		if p.Location.Value == nil {
			actor.Location = ""
		} else {
			actor.Location = *p.Location.Value
		}
	}
	if p.AvatarURL.IsSet() {
		if p.AvatarURL.Value == nil {
			actor.AvatarURL = ""
		} else {
			actor.AvatarURL = *p.AvatarURL.Value
		}
	}
	if p.BannerURL.IsSet() {
		if p.BannerURL.Value == nil {
			actor.BannerURL = ""
		} else {
			actor.BannerURL = *p.BannerURL.Value
		}
	}
	if p.Tags.IsSet() {
		if p.Tags.Value == nil {
			actor.Tags = nil
		} else {
			actor.Tags = append([]string(nil), (*p.Tags.Value)...)
		}
	}
	if p.EmojiNames.IsSet() {
		if p.EmojiNames.Value == nil {
			actor.EmojiNames = nil
		} else {
			actor.EmojiNames = append([]string(nil), (*p.EmojiNames.Value)...)
		}
	}
	if p.IsBot.IsSet() {
		actor.IsBot = p.IsBot.Value != nil && *p.IsBot.Value
	}
	if p.IsCat.IsSet() {
		actor.IsCat = p.IsCat.Value != nil && *p.IsCat.Value
	}
	if p.IsLocked.IsSet() {
		actor.IsLocked = p.IsLocked.Value != nil && *p.IsLocked.Value
	}
	if p.IsDiscoverable.IsSet() {
		actor.IsDiscoverable = p.IsDiscoverable.Value != nil && *p.IsDiscoverable.Value
	}
	return actor
}

type Lookup interface {
	FindLocalByID(context.Context, string) (*Actor, error)
	FindLocalByUsername(context.Context, string) (*Actor, error)
}

type Repository interface {
	Lookup
	FindOwnedLocalByID(context.Context, string, string) (*Actor, error)
	CreateOwnedLocalActor(context.Context, Actor) (*Actor, error)
	UpdateOwnedLocalActor(context.Context, string, string, ActorPatch) (*Actor, error)
	SuspendOwnedLocalActors(context.Context, string) (int64, error)
	ListOwnedAccountIDs(context.Context) ([]string, error)
	FindByURI(context.Context, string) (*Actor, error)
	FindAnyByURI(context.Context, string) (*Actor, error)
	FilterActiveRemoteIDs(context.Context, []string) (map[string]struct{}, error)
	FindByPublicKeyID(context.Context, string) (*Actor, error)
	UpsertRemoteActor(context.Context, Actor) (*Actor, error)
	AddRemoteFeaturedNote(context.Context, string, string, int) (*Actor, error)
	RemoveRemoteFeaturedNote(context.Context, string, string) (*Actor, error)
	MarkRemoteActorDeleted(context.Context, string) error
}
