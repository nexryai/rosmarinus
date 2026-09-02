package realtime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/nexryai/rosmarinus/internal/connector"
)

const channelNamespace = "rosmarinus:sse"

type Event struct {
	Version    int       `json:"version"`
	Type       string    `json:"type"`
	EventID    string    `json:"event_id"`
	ActorID    string    `json:"actor_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
	Data       any       `json:"data"`
}

type Subscription interface {
	Channel() <-chan []byte
	Close() error
}

type Broker interface {
	Publish(context.Context, string, string, string, any) error
	Subscribe(context.Context, string) (Subscription, error)
}

type RedisBroker struct {
	client *redis.Client
	now    func() time.Time
}

type DomainPublisher struct {
	broker Broker
	logger *log.Logger
}

type redisSubscription struct {
	pubsub    *redis.PubSub
	channel   <-chan *redis.Message
	output    chan []byte
	done      chan struct{}
	closeOnce sync.Once
}

func NewRedisBroker(client *redis.Client) *RedisBroker {
	return &RedisBroker{client: client, now: func() time.Time { return time.Now().UTC() }}
}

func NewDomainPublisher(broker Broker, logger *log.Logger) *DomainPublisher {
	return &DomainPublisher{broker: broker, logger: logger}
}

func (b *RedisBroker) Publish(ctx context.Context, accountID, eventType, actorID string, data any) error {
	if b == nil || b.client == nil {
		return nil
	}
	if strings.TrimSpace(accountID) == "" || strings.TrimSpace(eventType) == "" {
		return fmt.Errorf("realtime account and event type are required")
	}
	eventID, err := randomID()
	if err != nil {
		return err
	}
	payload, err := json.Marshal(Event{
		Version: 1, Type: eventType, EventID: eventID, ActorID: strings.TrimSpace(actorID),
		OccurredAt: b.now(), Data: data,
	})
	if err != nil {
		return err
	}
	return b.client.Publish(ctx, accountChannel(accountID), payload).Err()
}

func (b *RedisBroker) Subscribe(ctx context.Context, accountID string) (Subscription, error) {
	if b == nil || b.client == nil || strings.TrimSpace(accountID) == "" {
		return nil, fmt.Errorf("realtime broker and account are required")
	}
	pubsub := b.client.Subscribe(ctx, accountChannel(accountID))
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return nil, err
	}
	subscription := &redisSubscription{
		pubsub: pubsub, channel: pubsub.Channel(), output: make(chan []byte, 32), done: make(chan struct{}),
	}
	go subscription.forward()
	return subscription, nil
}

func (s *redisSubscription) Channel() <-chan []byte {
	return s.output
}

func (s *redisSubscription) Close() error {
	var err error
	s.closeOnce.Do(func() {
		close(s.done)
		err = s.pubsub.Close()
	})
	return err
}

func (s *redisSubscription) forward() {
	defer close(s.output)
	for {
		select {
		case <-s.done:
			return
		case message, ok := <-s.channel:
			if !ok {
				return
			}
			payload := []byte(message.Payload)
			select {
			case s.output <- payload:
			default:
				// Disconnect slow clients; REST reconciliation is the durable recovery path.
				_ = s.pubsub.Close()
				return
			}
		}
	}
}

func accountChannel(accountID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(accountID)))
	return channelNamespace + ":" + hex.EncodeToString(sum[:])
}

func randomID() (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate realtime event id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func (p *DomainPublisher) publish(ctx context.Context, accountID, eventType, actorID string, data any) error {
	if p == nil || p.broker == nil || accountID == "" {
		return nil
	}
	if err := p.broker.Publish(ctx, accountID, eventType, actorID, data); err != nil && p.logger != nil {
		p.logger.Printf("realtime: best-effort publish failed account_id=%s actor_id=%s type=%s err=%v", accountID, actorID, eventType, err)
	}
	return nil
}

func (p *DomainPublisher) PublishPostCreated(ctx context.Context, payload connector.PostCreated) error {
	return p.publish(ctx, payload.AccountID, "note.created", payload.ActorID, map[string]string{"note_id": payload.NoteID})
}

func (p *DomainPublisher) PublishNotificationCreated(ctx context.Context, payload connector.NotificationCreated) error {
	return p.publish(ctx, payload.AccountID, "notification.created", payload.RecipientActorID, map[string]string{"notification_id": payload.NotificationID})
}

func (p *DomainPublisher) PublishFollowApprovalRequested(ctx context.Context, payload connector.FollowApproval) error {
	return p.publish(ctx, payload.AccountID, "follow.approval.requested", payload.FolloweeID, map[string]string{"follower_id": payload.FollowerID})
}

func (p *DomainPublisher) PublishFollowApprovalCompleted(ctx context.Context, payload connector.FollowApproval) error {
	return p.publish(ctx, payload.AccountID, "follow.approval.completed", payload.FolloweeID, map[string]string{"follower_id": payload.FollowerID})
}

func (p *DomainPublisher) PublishFollowApprovalRejected(ctx context.Context, payload connector.FollowApproval) error {
	return p.publish(ctx, payload.AccountID, "follow.approval.rejected", payload.FolloweeID, map[string]string{"follower_id": payload.FollowerID})
}

var _ Broker = (*RedisBroker)(nil)
