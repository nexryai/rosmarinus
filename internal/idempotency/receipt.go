package idempotency

import (
	"context"
	"time"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

type Receipt struct {
	AccountID  string
	Key        string
	Operation  string
	ActorID    string
	IntentHash string
	Status     Status
	Result     any
	ErrorCode  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ExpiresAt  time.Time
}

type Store interface {
	Claim(context.Context, Receipt) (*Receipt, bool, error)
	Complete(context.Context, string, string, string, any, time.Time) error
	Fail(context.Context, string, string, string, time.Time) error
}
