package mongostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/notifications"
)

type NotificationRepository struct {
	collection *mongo.Collection
}

type notificationDocument struct {
	ID                 string     `bson:"_id"`
	LegacyIDs          []string   `bson:"legacyIds,omitempty"`
	RecipientAccountID string     `bson:"recipientAccountId,omitempty"`
	RecipientActorID   string     `bson:"recipientActorId"`
	Kind               string     `bson:"kind"`
	SourceActorID      string     `bson:"sourceActorId,omitempty"`
	NoteID             string     `bson:"noteId,omitempty"`
	RemoteActivityID   string     `bson:"remoteActivityId"`
	CreatedAt          time.Time  `bson:"createdAt"`
	IsRead             bool       `bson:"isRead"`
	ReadAt             *time.Time `bson:"readAt,omitempty"`
}

func NewNotificationRepository(db *mongo.Database) *NotificationRepository {
	return &NotificationRepository{collection: db.Collection("notifications")}
}

func (r *NotificationRepository) Upsert(ctx context.Context, notification notifications.Notification) (*notifications.Notification, error) {
	if notification.RecipientActorID == "" || notification.Kind == "" || notification.RemoteActivityID == "" {
		return nil, fmt.Errorf("notification recipient, kind, and remote activity id are required")
	}
	generatedID, err := newDocumentID(ctx, r.collection)
	if err != nil {
		return nil, fmt.Errorf("generate notification id: %w", err)
	}
	notification.ID = generatedID
	if notification.CreatedAt.IsZero() {
		notification.CreatedAt = time.Now().UTC()
	}
	doc := fromNotification(notification)
	key := bson.M{
		"recipientActorId": doc.RecipientActorID,
		"kind":             doc.Kind,
		"remoteActivityId": doc.RemoteActivityID,
	}
	_, err = r.collection.UpdateOne(ctx, key, bson.M{"$setOnInsert": doc}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.findOne(ctx, key)
}

func (r *NotificationRepository) MarkRead(ctx context.Context, accountID, actorID, notificationID string) (*notifications.Notification, error) {
	if accountID == "" || actorID == "" || notificationID == "" {
		return nil, fmt.Errorf("notification account, actor, and id are required")
	}
	now := time.Now().UTC()
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"$or":                bson.A{bson.M{"_id": notificationID}, bson.M{"legacyIds": notificationID}},
		"recipientAccountId": accountID, "recipientActorId": actorID,
		"isRead": bson.M{"$ne": true},
	}, bson.M{"$set": bson.M{"isRead": true, "readAt": now}})
	if err != nil {
		return nil, err
	}
	return r.findOne(ctx, bson.M{
		"$or":                bson.A{bson.M{"_id": notificationID}, bson.M{"legacyIds": notificationID}},
		"recipientAccountId": accountID, "recipientActorId": actorID,
	})
}

func (r *NotificationRepository) findOne(ctx context.Context, filter bson.M) (*notifications.Notification, error) {
	var doc notificationDocument
	if err := r.collection.FindOne(ctx, filter).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &notifications.Notification{
		ID: doc.ID, RecipientAccountID: doc.RecipientAccountID, RecipientActorID: doc.RecipientActorID,
		Kind: doc.Kind, SourceActorID: doc.SourceActorID, NoteID: doc.NoteID,
		RemoteActivityID: doc.RemoteActivityID, CreatedAt: doc.CreatedAt,
		IsRead: doc.IsRead, ReadAt: doc.ReadAt,
	}, nil
}

func fromNotification(notification notifications.Notification) notificationDocument {
	return notificationDocument{
		ID: notification.ID, RecipientAccountID: notification.RecipientAccountID,
		RecipientActorID: notification.RecipientActorID, Kind: notification.Kind,
		SourceActorID: notification.SourceActorID, NoteID: notification.NoteID,
		RemoteActivityID: notification.RemoteActivityID, CreatedAt: notification.CreatedAt,
		IsRead: notification.IsRead, ReadAt: notification.ReadAt,
	}
}
