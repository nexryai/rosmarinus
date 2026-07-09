package mongostore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/nexryai/rosmarinus/internal/domain/reports"
)

type ReportRepository struct {
	collection *mongo.Collection
}

type reportDocument struct {
	ID               string    `bson:"_id,omitempty"`
	TargetUserID     string    `bson:"targetUserId"`
	TargetUserHost   *string   `bson:"targetUserHost"`
	ReporterID       string    `bson:"reporterId"`
	ReporterHost     *string   `bson:"reporterHost"`
	ReporterURI      string    `bson:"reporterUri"`
	Content          string    `bson:"content,omitempty"`
	Comment          string    `bson:"comment"`
	ObjectURIs       []string  `bson:"objectUris,omitempty"`
	RemoteActivityID string    `bson:"remoteActivityId,omitempty"`
	CreatedAt        time.Time `bson:"createdAt"`
}

func NewReportRepository(db *mongo.Database) *ReportRepository {
	return &ReportRepository{collection: db.Collection("abuse_reports")}
}

func (r *ReportRepository) FindByRemoteActivityID(ctx context.Context, remoteActivityID string) (*reports.Report, error) {
	if remoteActivityID == "" {
		return nil, nil
	}
	return r.findOne(ctx, bson.M{"remoteActivityId": remoteActivityID})
}

func (r *ReportRepository) Create(ctx context.Context, report reports.Report) (*reports.Report, error) {
	if report.TargetUserID == "" || report.ReporterID == "" {
		return nil, fmt.Errorf("targetUserId and reporterId are required")
	}
	if report.RemoteActivityID != "" {
		existing, err := r.FindByRemoteActivityID(ctx, report.RemoteActivityID)
		if err != nil || existing != nil {
			return existing, err
		}
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now().UTC()
	}
	if report.ID == "" {
		report.ID = reportID(report)
	}
	doc := fromReport(report)
	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		if mongo.IsDuplicateKeyError(err) && report.RemoteActivityID != "" {
			return r.FindByRemoteActivityID(ctx, report.RemoteActivityID)
		}
		return nil, err
	}
	return r.findOne(ctx, bson.M{"_id": doc.ID})
}

func (r *ReportRepository) findOne(ctx context.Context, filter bson.M) (*reports.Report, error) {
	var doc reportDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toReport(doc), nil
}

func fromReport(report reports.Report) reportDocument {
	return reportDocument{
		ID:               report.ID,
		TargetUserID:     report.TargetUserID,
		TargetUserHost:   report.TargetUserHost,
		ReporterID:       report.ReporterID,
		ReporterHost:     report.ReporterHost,
		ReporterURI:      report.ReporterURI,
		Content:          report.Content,
		Comment:          report.Comment,
		ObjectURIs:       report.ObjectURIs,
		RemoteActivityID: report.RemoteActivityID,
		CreatedAt:        report.CreatedAt,
	}
}

func toReport(doc reportDocument) *reports.Report {
	return &reports.Report{
		ID:               doc.ID,
		TargetUserID:     doc.TargetUserID,
		TargetUserHost:   doc.TargetUserHost,
		ReporterID:       doc.ReporterID,
		ReporterHost:     doc.ReporterHost,
		ReporterURI:      doc.ReporterURI,
		Content:          doc.Content,
		Comment:          doc.Comment,
		ObjectURIs:       doc.ObjectURIs,
		RemoteActivityID: doc.RemoteActivityID,
		CreatedAt:        doc.CreatedAt,
	}
}

func reportID(report reports.Report) string {
	key := report.RemoteActivityID
	if key == "" {
		key = report.TargetUserID + "\x00" + report.ReporterID + "\x00" + report.CreatedAt.Format(time.RFC3339Nano)
	}
	sum := sha256.Sum256([]byte(key))
	return "report_" + hex.EncodeToString(sum[:])[:24]
}
