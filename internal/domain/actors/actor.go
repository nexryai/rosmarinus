package actors

import (
	"context"
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

type Lookup interface {
	FindLocalByID(context.Context, string) (*Actor, error)
	FindLocalByUsername(context.Context, string) (*Actor, error)
}

type Repository interface {
	Lookup
	FindOwnedLocalByID(context.Context, string, string) (*Actor, error)
	CreateOwnedLocalActor(context.Context, Actor) (*Actor, error)
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
