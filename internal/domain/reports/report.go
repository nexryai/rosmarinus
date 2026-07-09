package reports

import (
	"context"
	"time"
)

type Report struct {
	ID               string
	TargetUserID     string
	TargetUserHost   *string
	ReporterID       string
	ReporterHost     *string
	ReporterURI      string
	Content          string
	Comment          string
	ObjectURIs       []string
	RemoteActivityID string
	CreatedAt        time.Time
}

type Repository interface {
	FindByRemoteActivityID(context.Context, string) (*Report, error)
	Create(context.Context, Report) (*Report, error)
}
