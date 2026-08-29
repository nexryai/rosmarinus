package connector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ably/ably-go/ably"
)

const (
	DefaultAccountEventNamespace = "rosmarinus:accounts"

	EventPostCreated             = "post.created"
	EventNotificationCreated     = "notification.created"
	EventFollowApprovalRequested = "follow.approval.requested"
	EventFollowApprovalCompleted = "follow.approval.completed"
	EventFollowApprovalRejected  = "follow.approval.rejected"
	EventCommandSucceeded        = "command.succeeded"
	EventCommandFailed           = "command.failed"
	EventActorCreated            = "actor.created"
	EventActorUpdated            = "actor.updated"
	EventActorDeleted            = "actor.deleted"
)

type Channel interface {
	Publish(context.Context, string, interface{}) error
}

type ChannelProvider interface {
	Channel(string) Channel
}

type Publisher struct {
	fixedChannel     Channel
	channels         ChannelProvider
	accountNamespace string
	now              func() time.Time
}

type ablyRESTChannels struct {
	client *ably.REST
}

type ablyRESTChannel struct {
	channel *ably.RESTChannel
}

type Envelope struct {
	Version    int       `json:"version"`
	Type       string    `json:"type"`
	RequestID  string    `json:"request_id,omitempty"`
	ActorID    string    `json:"actor_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
	Data       any       `json:"data"`
}

type PostCreated struct {
	AccountID string `json:"-"`
	ActorID   string `json:"actor_id"`
	NoteID    string `json:"note_id"`
	URI       string `json:"uri"`
}

type NotificationCreated struct {
	AccountID        string `json:"-"`
	RecipientActorID string `json:"recipient_actor_id"`
	NotificationID   string `json:"notification_id"`
	Kind             string `json:"kind"`
	SourceActorID    string `json:"source_actor_id,omitempty"`
	NoteID           string `json:"note_id,omitempty"`
}

type FollowApproval struct {
	AccountID   string `json:"-"`
	FollowerID  string `json:"follower_id"`
	FolloweeID  string `json:"followee_id"`
	FollowerURI string `json:"follower_uri"`
	FolloweeURI string `json:"followee_uri"`
}

type CommandSucceeded struct {
	Command string `json:"command"`
	Result  any    `json:"result,omitempty"`
}

type CommandFailed struct {
	Command string `json:"command"`
	Code    string `json:"code"`
}

func NewPublisher(channel Channel) *Publisher {
	return &Publisher{
		fixedChannel: channel,
		now:          func() time.Time { return time.Now().UTC() },
	}
}

func NewAccountPublisher(channels ChannelProvider, namespace string) *Publisher {
	namespace = strings.Trim(strings.TrimSpace(namespace), ":")
	if namespace == "" {
		namespace = DefaultAccountEventNamespace
	}
	return &Publisher{
		channels:         channels,
		accountNamespace: namespace,
		now:              func() time.Time { return time.Now().UTC() },
	}
}

func NewAblyPublisher(apiKey, namespace string) (*Publisher, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("ably api key is required")
	}
	client, err := ably.NewREST(ably.WithKey(apiKey))
	if err != nil {
		return nil, err
	}
	return NewAccountPublisher(ablyRESTChannels{client: client}, namespace), nil
}

func (c ablyRESTChannels) Channel(name string) Channel {
	return ablyRESTChannel{channel: c.client.Channels.Get(name)}
}

func (c ablyRESTChannel) Publish(ctx context.Context, name string, data interface{}) error {
	return c.channel.Publish(ctx, name, data)
}

func (p *Publisher) Publish(ctx context.Context, eventType string, data any) error {
	if p == nil || p.fixedChannel == nil {
		return nil
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return fmt.Errorf("connector event type is required")
	}
	envelope := Envelope{
		Version:    1,
		Type:       eventType,
		OccurredAt: p.now(),
		Data:       data,
	}
	return p.fixedChannel.Publish(ctx, eventType, envelope)
}

func (p *Publisher) PublishAccount(ctx context.Context, accountID, eventType, requestID, actorID string, data any) error {
	if p == nil {
		return nil
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return fmt.Errorf("connector event type is required")
	}
	var channel Channel
	if p.fixedChannel != nil {
		channel = p.fixedChannel
	} else {
		name, err := p.AccountEventChannel(accountID)
		if err != nil {
			return err
		}
		if p.channels == nil {
			return fmt.Errorf("connector account event channels are not configured")
		}
		channel = p.channels.Channel(name)
	}
	envelope := Envelope{
		Version:    1,
		Type:       eventType,
		RequestID:  strings.TrimSpace(requestID),
		ActorID:    strings.TrimSpace(actorID),
		OccurredAt: p.now(),
		Data:       data,
	}
	return channel.Publish(ctx, eventType, envelope)
}

func (p *Publisher) AccountEventChannel(accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if !validChannelSegment(accountID) {
		return "", fmt.Errorf("invalid connector account id")
	}
	namespace := strings.Trim(strings.TrimSpace(p.accountNamespace), ":")
	if namespace == "" {
		namespace = DefaultAccountEventNamespace
	}
	return namespace + ":" + accountID + ":events", nil
}

func validChannelSegment(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func (p *Publisher) PublishPostCreated(ctx context.Context, payload PostCreated) error {
	if p != nil && p.fixedChannel == nil && strings.TrimSpace(payload.AccountID) == "" {
		return nil
	}
	return p.PublishAccount(ctx, payload.AccountID, EventPostCreated, "", payload.ActorID, payload)
}

func (p *Publisher) PublishNotificationCreated(ctx context.Context, payload NotificationCreated) error {
	if p != nil && p.fixedChannel == nil && strings.TrimSpace(payload.AccountID) == "" {
		return nil
	}
	return p.PublishAccount(ctx, payload.AccountID, EventNotificationCreated, "", payload.RecipientActorID, payload)
}

func (p *Publisher) PublishFollowApprovalRequested(ctx context.Context, payload FollowApproval) error {
	if p != nil && p.fixedChannel == nil && strings.TrimSpace(payload.AccountID) == "" {
		return nil
	}
	return p.PublishAccount(ctx, payload.AccountID, EventFollowApprovalRequested, "", payload.FolloweeID, payload)
}

func (p *Publisher) PublishFollowApprovalCompleted(ctx context.Context, payload FollowApproval) error {
	if p != nil && p.fixedChannel == nil && strings.TrimSpace(payload.AccountID) == "" {
		return nil
	}
	return p.PublishAccount(ctx, payload.AccountID, EventFollowApprovalCompleted, "", payload.FolloweeID, payload)
}

func (p *Publisher) PublishFollowApprovalRejected(ctx context.Context, payload FollowApproval) error {
	if p != nil && p.fixedChannel == nil && strings.TrimSpace(payload.AccountID) == "" {
		return nil
	}
	return p.PublishAccount(ctx, payload.AccountID, EventFollowApprovalRejected, "", payload.FolloweeID, payload)
}

func (p *Publisher) PublishCommandSucceeded(ctx context.Context, accountID, requestID, actorID, command string, result any) error {
	return p.PublishAccount(ctx, accountID, EventCommandSucceeded, requestID, actorID, CommandSucceeded{
		Command: command,
		Result:  result,
	})
}

func (p *Publisher) PublishCommandFailed(ctx context.Context, accountID, requestID, actorID, command, code string) error {
	return p.PublishAccount(ctx, accountID, EventCommandFailed, requestID, actorID, CommandFailed{
		Command: command,
		Code:    code,
	})
}

func (p *Publisher) PublishActorCreated(ctx context.Context, accountID, requestID string, payload ActorCreated) error {
	return p.PublishAccount(ctx, accountID, EventActorCreated, requestID, payload.ActorID, payload)
}

func (p *Publisher) PublishActorUpdated(ctx context.Context, accountID, requestID string, payload ActorUpdated) error {
	return p.PublishAccount(ctx, accountID, EventActorUpdated, requestID, payload.ActorID, payload)
}

func (p *Publisher) PublishActorDeleted(ctx context.Context, accountID, requestID string, payload ActorDeleted) error {
	return p.PublishAccount(ctx, accountID, EventActorDeleted, requestID, payload.ActorID, payload)
}
