package account

import (
	"context"
	"time"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusDeleted   Status = "deleted"
)

type Account struct {
	ID            string
	AblyClientID  string
	Status        Status
	AuthzRevision int64
	DeletedAt     *time.Time
}

func (a *Account) IsActive() bool {
	return a != nil && a.Status == StatusActive && a.DeletedAt == nil
}

type Repository interface {
	FindActiveByAblyClientID(context.Context, string) (*Account, error)
	FindByID(context.Context, string) (*Account, error)
}
