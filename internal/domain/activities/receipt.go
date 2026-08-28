package activities

import (
	"context"
	"time"
)

type Claim struct {
	ActivityID string
	ActorURI   string
	Token      string
}

type ReceiptRepository interface {
	Claim(context.Context, string, string, time.Time, time.Duration, time.Duration) (*Claim, bool, error)
	Complete(context.Context, Claim, time.Time) error
	Release(context.Context, Claim) error
}
