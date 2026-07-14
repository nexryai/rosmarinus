package connector

import (
	"context"
	"time"
)

type CommandReceiptStatus string

const (
	CommandReceiptPending   CommandReceiptStatus = "pending"
	CommandReceiptCompleted CommandReceiptStatus = "completed"
	CommandReceiptFailed    CommandReceiptStatus = "failed"
)

type CommandReceipt struct {
	AccountID string
	ClientID  string
	RequestID string
	Command   string
	ActorID   string
	Status    CommandReceiptStatus
	Result    any
	ErrorCode string
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
}

type CommandReceiptStore interface {
	Claim(context.Context, CommandReceipt) (*CommandReceipt, bool, error)
	Complete(context.Context, string, string, string, any, time.Time) error
	Fail(context.Context, string, string, string, time.Time) error
}
