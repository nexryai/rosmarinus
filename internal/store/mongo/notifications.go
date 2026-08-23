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
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/nexryai/rosmarinus/internal/domain/notifications"
)

type NotificationRepository struct {
	collection *mongo.Collection
}

type notificationDocument struct {
	ID                 string     `bson:"_id"`
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
	if notification.ID == "" {
		notification.ID = notificationID(notification.RecipientActorID, notification.Kind, notification.RemoteActivityID)
	}
	if notification.CreatedAt.IsZero() {
		notification.CreatedAt = time.Now().UTC()
	}
	doc := fromNotification(notification)
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": doc.ID}, bson.M{"$setOnInsert": doc}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return nil, err
	}
	return r.findOne(ctx, bson.M{"_id": doc.ID})
}

func (r *NotificationRepository) MarkRead(ctx context.Context, accountID, actorID, notificationID string) (*notifications.Notification, error) {
	if accountID == "" || actorID == "" || notificationID == "" {
		return nil, fmt.Errorf("notification account, actor, and id are required")
	}
	now := time.Now().UTC()
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"_id": notificationID, "recipientAccountId": accountID, "recipientActorId": actorID,
		"isRead": bson.M{"$ne": true},
	}, bson.M{"$set": bson.M{"isRead": true, "readAt": now}})
	if err != nil {
		return nil, err
	}
	return r.findOne(ctx, bson.M{"_id": notificationID, "recipientAccountId": accountID, "recipientActorId": actorID})
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

func notificationID(recipientActorID, kind, remoteActivityID string) string {
	sum := sha256.Sum256([]byte(recipientActorID + "\x00" + kind + "\x00" + remoteActivityID))
	return "notification_" + hex.EncodeToString(sum[:])[:24]
}
