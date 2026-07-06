package actors

import "context"

type Actor struct {
	ID            string
	Username      string
	UsernameLower string
	Host          *string
	URI           string
	IsSuspended   bool
}

type Lookup interface {
	FindLocalByID(context.Context, string) (*Actor, error)
	FindLocalByUsername(context.Context, string) (*Actor, error)
}
