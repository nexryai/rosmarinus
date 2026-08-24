package instances

import (
	"context"
	"time"
)

const (
	SuspensionNone              = "none"
	SuspensionManual            = "manuallySuspended"
	SuspensionGone              = "goneSuspended"
	SuspensionAutoNotResponding = "autoSuspendedForNotResponding"
)

type Instance struct {
	ID                      string
	Host                    string
	UsersCount              int64
	NotesCount              int64
	FollowingCount          int64
	FollowersCount          int64
	LatestRequestReceivedAt *time.Time
	LatestRequestSentAt     *time.Time
	LatestStatus            int
	IsNotResponding         bool
	NotRespondingSince      *time.Time
	SuspensionState         string
	SoftwareName            string
	SoftwareVersion         string
	OpenRegistrations       *bool
	Name                    string
	Description             string
	MaintainerName          string
	MaintainerEmail         string
	IconURL                 string
	FaviconURL              string
	ThemeColor              string
	FirstRetrievedAt        time.Time
	InfoUpdatedAt           *time.Time
	UpdatedAt               time.Time
}

type Metadata struct {
	NodeInfoFetched   bool
	SoftwareName      string
	SoftwareVersion   string
	OpenRegistrations *bool
	UsersCount        int64
	NotesCount        int64
	Name              string
	Description       string
	MaintainerName    string
	MaintainerEmail   string
	IconURL           string
	FaviconURL        string
	ThemeColor        string
}

type Repository interface {
	FindByHost(context.Context, string) (*Instance, error)
	Register(context.Context, string, time.Time) (*Instance, bool, error)
	RecordReceived(context.Context, string, time.Time) (*Instance, error)
	RecordDeliverySuccess(context.Context, string, time.Time, int) (*Instance, error)
	RecordDeliveryFailure(context.Context, string, time.Time, int) (*Instance, error)
	UpdateMetadata(context.Context, string, Metadata, time.Time) (*Instance, error)
	RefreshRelationshipCounts(context.Context, string, time.Time) (*Instance, error)
	SuspendGone(context.Context, string, time.Time) (*Instance, error)
}
