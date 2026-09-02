package settings

import (
	"context"
	"time"
)

const DefaultTheme = "yellow"

type Account struct {
	AccountID       string
	Theme           string
	ReduceMotion    bool
	CompactMode     bool
	SelectedActorID string
	UpdatedAt       time.Time
}

type Actor struct {
	AccountID          string
	ActorID            string
	DefaultVisibility  string
	ShowContentWarning bool
	DisplayOrder       int
	Color              string
	Pinned             bool
	UpdatedAt          time.Time
}

type AccountPatch struct {
	Theme           *string
	ReduceMotion    *bool
	CompactMode     *bool
	SelectedActorID *string
}

type ActorPatch struct {
	DefaultVisibility  *string
	ShowContentWarning *bool
	DisplayOrder       *int
	Color              *string
	Pinned             *bool
}

type Repository interface {
	GetAccount(context.Context, string) (*Account, error)
	UpdateAccount(context.Context, string, AccountPatch) (*Account, error)
	GetActor(context.Context, string, string) (*Actor, error)
	UpdateActor(context.Context, string, string, ActorPatch) (*Actor, error)
}
