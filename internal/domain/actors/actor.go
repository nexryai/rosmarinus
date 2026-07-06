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
	PublicKeyID   string
	PublicKeyPEM  string
	PrivateKeyPEM string
	IsSuspended   bool
}

type Lookup interface {
	FindLocalByID(context.Context, string) (*Actor, error)
	FindLocalByUsername(context.Context, string) (*Actor, error)
}
