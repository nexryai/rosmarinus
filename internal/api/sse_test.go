package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/realtime"
)

type fakeEventBroker struct {
	accountID string
	events    chan []byte
}

func (b *fakeEventBroker) Publish(context.Context, string, string, string, any) error {
	return nil
}

func (b *fakeEventBroker) Subscribe(_ context.Context, accountID string) (realtime.Subscription, error) {
	b.accountID = accountID
	return &fakeEventSubscription{events: b.events}, nil
}

type fakeEventSubscription struct {
	events <-chan []byte
}

func (s *fakeEventSubscription) Channel() <-chan []byte { return s.events }
func (s *fakeEventSubscription) Close() error           { return nil }

func TestEventStreamUsesAuthenticatedAccount(t *testing.T) {
	events := make(chan []byte, 1)
	payload, err := json.Marshal(realtime.Event{
		Version: 1, Type: "notification.created", EventID: "event-1", ActorID: "actor-1",
		OccurredAt: time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC), Data: map[string]string{"notification_id": "notification-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	events <- payload
	close(events)
	broker := &fakeEventBroker{events: events}
	handler := NewHandlerWithRealtime(
		fakeAuthenticator{session: &Session{AccountID: "account-1"}}, &fakeActorStore{}, &fakeExecutor{}, nil,
		nil, nil, InstanceInfo{}, broker, nil, nil, 0,
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/events", nil))
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "text/event-stream; charset=utf-8" {
		t.Fatalf("status/headers = %d %v", recorder.Code, recorder.Header())
	}
	if broker.accountID != "account-1" {
		t.Fatalf("subscription account = %q", broker.accountID)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "id: event-1\nevent: notification.created\ndata: ") || !strings.Contains(body, `"actor_id":"actor-1"`) {
		t.Fatalf("unexpected SSE body: %s", body)
	}
}

func TestWriteSSEEventRejectsHeaderInjection(t *testing.T) {
	payload := []byte(`{"version":1,"type":"note.created\nevent: forged","event_id":"event-1","occurred_at":"2026-09-02T12:00:00Z","data":{}}`)
	if err := writeSSEEvent(httptest.NewRecorder(), payload); err == nil {
		t.Fatal("expected injected event type to be rejected")
	}
}
