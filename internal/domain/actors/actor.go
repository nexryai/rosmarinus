package actors

import "context"

type Actor struct {
	ID            string
	Username      string
	UsernameLower string
	Name          string
	Type          string
	Host          *string
	URI           string
	Inbox         string
	SharedInbox   string
	FollowersURI  string
	FollowingURI  string
	FeaturedURI   string
	PublicKeyID   string
	PublicKeyPEM  string
	PrivateKeyPEM string
	IsSuspended   bool
}

type Lookup interface {
	FindLocalByID(context.Context, string) (*Actor, error)
	FindLocalByUsername(context.Context, string) (*Actor, error)
}

type Repository interface {
	Lookup
	FindByURI(context.Context, string) (*Actor, error)
	FindByPublicKeyID(context.Context, string) (*Actor, error)
	UpsertRemoteActor(context.Context, Actor) (*Actor, error)
}
