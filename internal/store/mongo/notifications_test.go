package mongostore

import (
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/notifications"
)

func TestNotificationDocumentPreservesReadContract(t *testing.T) {
	readAt := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	doc := fromNotification(notifications.Notification{
		ID:                 "notification-1",
		RecipientAccountID: "account-1",
		RecipientActorID:   "actor-1",
		Kind:               notifications.KindReply,
		SourceActorID:      "remote-1",
		NoteID:             "note-1",
		RemoteActivityID:   "https://remote.example/activities/1",
		IsRead:             true,
		ReadAt:             &readAt,
	})
	if doc.RecipientAccountID != "account-1" || doc.RecipientActorID != "actor-1" || !doc.IsRead || doc.ReadAt == nil {
		t.Fatalf("unexpected notification document: %+v", doc)
	}
}

func TestNotificationIDIsStableAndRecipientScoped(t *testing.T) {
	first := notificationID("actor-1", notifications.KindMention, "https://remote.example/activities/1")
	if first != notificationID("actor-1", notifications.KindMention, "https://remote.example/activities/1") {
		t.Fatal("notification ID is not stable")
	}
	if first == notificationID("actor-2", notifications.KindMention, "https://remote.example/activities/1") {
		t.Fatal("notification ID must be scoped to the recipient Actor")
	}
}
