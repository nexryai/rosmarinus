package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/ably/ably-go/ably"
)

type AblyCommandSource struct {
	client  *ably.Realtime
	channel *ably.RealtimeChannel
}

func NewAblyCommandSource(apiKey, channelName string) (*AblyCommandSource, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("ably api key is required")
	}
	if strings.TrimSpace(channelName) == "" {
		channelName = DefaultChannel
	}
	client, err := ably.NewRealtime(ably.WithKey(apiKey))
	if err != nil {
		return nil, err
	}
	return &AblyCommandSource{
		client:  client,
		channel: client.Channels.Get(channelName),
	}, nil
}

func (s *AblyCommandSource) Subscribe(ctx context.Context, name string, handle func(CommandMessage)) (func(), error) {
	if s == nil || s.channel == nil {
		return func() {}, nil
	}
	unsubscribe, err := s.channel.Subscribe(ctx, name, func(message *ably.Message) {
		handle(CommandMessage{Name: message.Name, Data: message.Data})
	})
	if err != nil {
		return nil, err
	}
	return unsubscribe, nil
}

func (s *AblyCommandSource) Close() {
	if s != nil && s.client != nil {
		s.client.Close()
	}
}
