package realtime

import (
	"context"
	"errors"
	"testing"

	"github.com/nexryai/rosmarinus/internal/connector"
)

type failingBroker struct{}

func (failingBroker) Publish(context.Context, string, string, string, any) error {
	return errors.New("redis unavailable")
}

func (failingBroker) Subscribe(context.Context, string) (Subscription, error) {
	return nil, errors.New("not implemented")
}

func TestAccountChannelIsOpaqueAndAccountScoped(t *testing.T) {
	first := accountChannel("account-1")
	if first == accountChannel("account-2") {
		t.Fatal("different accounts shared a channel")
	}
	if first != accountChannel("account-1") {
		t.Fatal("channel derivation is not stable")
	}
	if first == channelNamespace+":account-1" {
		t.Fatal("raw account ID was exposed in channel name")
	}
}

func TestDomainPublisherDoesNotFailDurableOperationWhenPubSubIsUnavailable(t *testing.T) {
	publisher := NewDomainPublisher(failingBroker{}, nil)
	if err := publisher.PublishPostCreated(context.Background(), connector.PostCreated{
		AccountID: "account-1", ActorID: "actor-1", NoteID: "note-1",
	}); err != nil {
		t.Fatalf("best-effort publication returned error: %v", err)
	}
}
