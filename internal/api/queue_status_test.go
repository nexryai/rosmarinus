package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/queue"
)

type fakeQueueStatusReader struct {
	called bool
	err    error
}

func (f *fakeQueueStatusReader) Snapshot(context.Context) (queue.Snapshot, error) {
	f.called = true
	return queue.Snapshot{Queues: []queue.Status{{Name: queue.QueueInbox, Active: 2, Retry: 3, Pending: 4}}, UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, f.err
}

func TestQueueOverviewRequiresSessionAndReturnsCounts(t *testing.T) {
	reader := &fakeQueueStatusReader{}
	handler := NewHandlerCompleteWithMediaProxy(fakeAuthenticator{}, nil, nil, nil, nil, nil, InstanceInfo{}, nil, nil, nil, nil, nil, nil, 0, nil, nil, 0, HandlerExtras{QueueStatus: reader})
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/system/queues", nil))
	if unauthorized.Code != http.StatusUnauthorized || reader.called {
		t.Fatalf("unauthorized status=%d called=%t", unauthorized.Code, reader.called)
	}

	handler = NewHandlerCompleteWithMediaProxy(fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf"}}, nil, nil, nil, nil, nil, InstanceInfo{}, nil, nil, nil, nil, nil, nil, 0, nil, nil, 0, HandlerExtras{QueueStatus: reader})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/queues", nil))
	if response.Code != http.StatusOK || !reader.called {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Version int            `json:"version"`
		Data    queue.Snapshot `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != 1 || len(body.Data.Queues) != 1 || body.Data.Queues[0].Retry != 3 {
		t.Fatalf("response = %+v", body)
	}
}

func TestQueueOverviewFailureReturnsServerError(t *testing.T) {
	reader := &fakeQueueStatusReader{err: errors.New("redis unavailable")}
	handler := NewHandlerCompleteWithMediaProxy(fakeAuthenticator{session: &Session{AccountID: "account-1", CSRFToken: "csrf"}}, nil, nil, nil, nil, nil, InstanceInfo{}, nil, nil, nil, nil, nil, nil, 0, nil, nil, 0, HandlerExtras{QueueStatus: reader})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/system/queues", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
