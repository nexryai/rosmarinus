package account

import (
	"context"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusDeleted   Status = "deleted"
)

type Account struct {
	ID            string
	Username      string
	DisplayName   string
	WebAuthnID    []byte
	Status        Status
	AuthzRevision int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
}

func (a *Account) IsActive() bool {
	return a != nil && a.Status == StatusActive && a.DeletedAt == nil
}

type Repository interface {
	FindByID(context.Context, string) (*Account, error)
}
