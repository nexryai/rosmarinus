package mongostore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/net/idna"

	"github.com/nexryai/rosmarinus/internal/domain/instances"
)

const instanceAutoSuspendAfter = 7 * 24 * time.Hour

type InstanceRepository struct {
	collection *mongo.Collection
	follows    *mongo.Collection
}

type instanceDocument struct {
	ID                      string     `bson:"_id"`
	Host                    string     `bson:"host"`
	UsersCount              int64      `bson:"usersCount"`
	NotesCount              int64      `bson:"notesCount"`
	FollowingCount          int64      `bson:"followingCount"`
	FollowersCount          int64      `bson:"followersCount"`
	LatestRequestReceivedAt *time.Time `bson:"latestRequestReceivedAt,omitempty"`
	LatestRequestSentAt     *time.Time `bson:"latestRequestSentAt,omitempty"`
	LatestStatus            int        `bson:"latestStatus,omitempty"`
	IsNotResponding         bool       `bson:"isNotResponding"`
	NotRespondingSince      *time.Time `bson:"notRespondingSince,omitempty"`
	SuspensionState         string     `bson:"suspensionState"`
	SoftwareName            string     `bson:"softwareName,omitempty"`
	SoftwareVersion         string     `bson:"softwareVersion,omitempty"`
	OpenRegistrations       *bool      `bson:"openRegistrations,omitempty"`
	Name                    string     `bson:"name,omitempty"`
	Description             string     `bson:"description,omitempty"`
	MaintainerName          string     `bson:"maintainerName,omitempty"`
	MaintainerEmail         string     `bson:"maintainerEmail,omitempty"`
	IconURL                 string     `bson:"iconUrl,omitempty"`
	FaviconURL              string     `bson:"faviconUrl,omitempty"`
	ThemeColor              string     `bson:"themeColor,omitempty"`
	FirstRetrievedAt        time.Time  `bson:"firstRetrievedAt"`
	InfoUpdatedAt           *time.Time `bson:"infoUpdatedAt,omitempty"`
	UpdatedAt               time.Time  `bson:"updatedAt"`
}

func NewInstanceRepository(db *mongo.Database) *InstanceRepository {
	return &InstanceRepository{
		collection: db.Collection("instances"),
		follows:    db.Collection("follows"),
	}
}

func (r *InstanceRepository) FindByHost(ctx context.Context, host string) (*instances.Instance, error) {
	host, err := normalizeInstanceHost(host)
	if err != nil {
		return nil, err
	}
	var doc instanceDocument
	if err := r.collection.FindOne(ctx, bson.M{"host": host}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return toInstance(doc), nil
}

func (r *InstanceRepository) Register(ctx context.Context, host string, now time.Time) (*instances.Instance, bool, error) {
	host, err := normalizeInstanceHost(host)
	if err != nil {
		return nil, false, err
	}
	now = now.UTC()
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": instanceID(host)}, bson.M{
		"$setOnInsert": bson.M{
			"_id": instanceID(host), "host": host, "firstRetrievedAt": now,
			"suspensionState": instances.SuspensionNone, "isNotResponding": false,
			"usersCount": int64(0), "notesCount": int64(0),
			"followingCount": int64(0), "followersCount": int64(0), "updatedAt": now,
		},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, false, err
	}
	instance, err := r.FindByHost(ctx, host)
	return instance, result.UpsertedCount > 0, err
}

func (r *InstanceRepository) RecordReceived(ctx context.Context, host string, now time.Time) (*instances.Instance, error) {
	return r.updateContact(ctx, host, now, bson.M{
		"latestRequestReceivedAt": now.UTC(),
		"isNotResponding":         false,
		"notRespondingSince":      nil,
	}, true)
}

func (r *InstanceRepository) RecordDeliverySuccess(ctx context.Context, host string, now time.Time, status int) (*instances.Instance, error) {
	return r.updateContact(ctx, host, now, bson.M{
		"latestRequestSentAt": now.UTC(), "latestStatus": status,
		"isNotResponding": false, "notRespondingSince": nil,
	}, false)
}

func (r *InstanceRepository) RecordDeliveryFailure(ctx context.Context, host string, now time.Time, status int) (*instances.Instance, error) {
	host, err := normalizeInstanceHost(host)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	cutoff := now.Add(-instanceAutoSuspendAfter)
	state := bson.M{"$ifNull": bson.A{"$suspensionState", instances.SuspensionNone}}
	notRespondingSince := bson.M{"$ifNull": bson.A{"$notRespondingSince", now}}
	pipeline := mongo.Pipeline{
		{{Key: "$set", Value: bson.M{
			"host":                host,
			"firstRetrievedAt":    bson.M{"$ifNull": bson.A{"$firstRetrievedAt", now}},
			"latestRequestSentAt": now, "latestStatus": status,
			"isNotResponding": true, "notRespondingSince": notRespondingSince,
			"suspensionState": bson.M{"$cond": bson.A{
				bson.M{"$and": bson.A{
					bson.M{"$eq": bson.A{state, instances.SuspensionNone}},
					bson.M{"$lte": bson.A{notRespondingSince, cutoff}},
				}},
				instances.SuspensionAutoNotResponding,
				state,
			}},
			"updatedAt": now,
		}}},
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": instanceID(host)}, pipeline, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindByHost(ctx, host)
}

func (r *InstanceRepository) UpdateMetadata(ctx context.Context, host string, metadata instances.Metadata, now time.Time) (*instances.Instance, error) {
	host, err := normalizeInstanceHost(host)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	followingCount, followersCount, err := r.relationshipCounts(ctx, host)
	if err != nil {
		return nil, err
	}
	set := bson.M{
		"followingCount": followingCount,
		"followersCount": followersCount,
		"infoUpdatedAt":  now,
		"updatedAt":      now,
	}
	if metadata.NodeInfoFetched {
		set["softwareName"] = strings.ToLower(metadata.SoftwareName)
		set["softwareVersion"] = metadata.SoftwareVersion
		set["openRegistrations"] = metadata.OpenRegistrations
		set["usersCount"] = metadata.UsersCount
		set["notesCount"] = metadata.NotesCount
		set["maintainerName"] = metadata.MaintainerName
		set["maintainerEmail"] = metadata.MaintainerEmail
	}
	setNonEmpty(set, "name", metadata.Name)
	setNonEmpty(set, "description", metadata.Description)
	setNonEmpty(set, "iconUrl", metadata.IconURL)
	setNonEmpty(set, "faviconUrl", metadata.FaviconURL)
	setNonEmpty(set, "themeColor", metadata.ThemeColor)
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": instanceID(host)}, bson.M{
		"$set": set,
		"$setOnInsert": bson.M{
			"_id": instanceID(host), "host": host, "firstRetrievedAt": now,
			"suspensionState": instances.SuspensionNone, "isNotResponding": false,
		},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindByHost(ctx, host)
}

func (r *InstanceRepository) RefreshRelationshipCounts(ctx context.Context, host string, now time.Time) (*instances.Instance, error) {
	host, err := normalizeInstanceHost(host)
	if err != nil {
		return nil, err
	}
	followingCount, followersCount, err := r.relationshipCounts(ctx, host)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": instanceID(host)}, bson.M{
		"$set": bson.M{
			"followingCount": followingCount,
			"followersCount": followersCount,
			"updatedAt":      now,
		},
		"$setOnInsert": bson.M{
			"_id": instanceID(host), "host": host, "firstRetrievedAt": now,
			"suspensionState": instances.SuspensionNone, "isNotResponding": false,
			"usersCount": int64(0), "notesCount": int64(0),
		},
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindByHost(ctx, host)
}

func (r *InstanceRepository) SuspendGone(ctx context.Context, host string, now time.Time) (*instances.Instance, error) {
	return r.updateContact(ctx, host, now, bson.M{
		"suspensionState": instances.SuspensionGone,
	}, false)
}

func (r *InstanceRepository) relationshipCounts(ctx context.Context, host string) (int64, int64, error) {
	followingCount, err := r.follows.CountDocuments(ctx, bson.M{
		"followerHost": host,
		"status":       "accepted",
		"deletedAt":    nil,
	})
	if err != nil {
		return 0, 0, err
	}
	followersCount, err := r.follows.CountDocuments(ctx, bson.M{
		"followeeHost": host,
		"status":       "accepted",
		"deletedAt":    nil,
	})
	if err != nil {
		return 0, 0, err
	}
	return followingCount, followersCount, nil
}

func (r *InstanceRepository) updateContact(ctx context.Context, host string, now time.Time, set bson.M, unsuspendAuto bool) (*instances.Instance, error) {
	host, err := normalizeInstanceHost(host)
	if err != nil {
		return nil, err
	}
	now = now.UTC()
	set["updatedAt"] = now
	if unsuspendAuto {
		set["suspensionState"] = bson.M{"$cond": bson.A{
			bson.M{"$eq": bson.A{bson.M{"$ifNull": bson.A{"$suspensionState", instances.SuspensionNone}}, instances.SuspensionAutoNotResponding}},
			instances.SuspensionNone,
			bson.M{"$ifNull": bson.A{"$suspensionState", instances.SuspensionNone}},
		}}
		pipeline := mongo.Pipeline{{{Key: "$set", Value: mergeInstanceContactDefaults(set, host, now)}}}
		_, err = r.collection.UpdateOne(ctx, bson.M{"_id": instanceID(host)}, pipeline, options.UpdateOne().SetUpsert(true))
		if err != nil {
			return nil, err
		}
		return r.FindByHost(ctx, host)
	}
	setOnInsert := bson.M{
		"_id": instanceID(host), "host": host, "firstRetrievedAt": now,
		"usersCount": int64(0), "notesCount": int64(0),
		"followingCount": int64(0), "followersCount": int64(0),
	}
	if _, setsSuspensionState := set["suspensionState"]; !setsSuspensionState {
		setOnInsert["suspensionState"] = instances.SuspensionNone
	}
	_, err = r.collection.UpdateOne(ctx, bson.M{"_id": instanceID(host)}, bson.M{
		"$set":         set,
		"$setOnInsert": setOnInsert,
	}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.FindByHost(ctx, host)
}

func mergeInstanceContactDefaults(set bson.M, host string, now time.Time) bson.M {
	set["host"] = host
	set["firstRetrievedAt"] = bson.M{"$ifNull": bson.A{"$firstRetrievedAt", now}}
	set["usersCount"] = bson.M{"$ifNull": bson.A{"$usersCount", int64(0)}}
	set["notesCount"] = bson.M{"$ifNull": bson.A{"$notesCount", int64(0)}}
	set["followingCount"] = bson.M{"$ifNull": bson.A{"$followingCount", int64(0)}}
	set["followersCount"] = bson.M{"$ifNull": bson.A{"$followersCount", int64(0)}}
	return set
}

func setNonEmpty(set bson.M, field, value string) {
	if value != "" {
		set[field] = value
	}
}

func normalizeInstanceHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	host, err := idna.Lookup.ToASCII(host)
	if err != nil || host == "" || len(host) > 253 {
		return "", fmt.Errorf("invalid instance host %q", host)
	}
	return host, nil
}

func instanceID(host string) string {
	sum := sha256.Sum256([]byte(host))
	return "instance_" + hex.EncodeToString(sum[:])[:24]
}

func toInstance(doc instanceDocument) *instances.Instance {
	return &instances.Instance{
		ID: doc.ID, Host: doc.Host, UsersCount: doc.UsersCount, NotesCount: doc.NotesCount,
		FollowingCount: doc.FollowingCount, FollowersCount: doc.FollowersCount,
		LatestRequestReceivedAt: doc.LatestRequestReceivedAt, LatestRequestSentAt: doc.LatestRequestSentAt,
		LatestStatus: doc.LatestStatus, IsNotResponding: doc.IsNotResponding,
		NotRespondingSince: doc.NotRespondingSince, SuspensionState: doc.SuspensionState,
		SoftwareName: doc.SoftwareName, SoftwareVersion: doc.SoftwareVersion,
		OpenRegistrations: doc.OpenRegistrations, Name: doc.Name, Description: doc.Description,
		MaintainerName: doc.MaintainerName, MaintainerEmail: doc.MaintainerEmail,
		IconURL: doc.IconURL, FaviconURL: doc.FaviconURL, ThemeColor: doc.ThemeColor,
		FirstRetrievedAt: doc.FirstRetrievedAt, InfoUpdatedAt: doc.InfoUpdatedAt, UpdatedAt: doc.UpdatedAt,
	}
}
