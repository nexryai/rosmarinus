package connector

import (
	"context"
	"testing"
)

type fakeCommandSource struct {
	name   string
	handle func(CommandMessage)
}

func (f *fakeCommandSource) Subscribe(ctx context.Context, name string, handle func(CommandMessage)) (func(), error) {
	_ = ctx
	f.name = name
	f.handle = handle
	return func() { f.handle = nil }, nil
}

type fakeFollowApprover struct {
	followerID string
	followeeID string
}

func (f *fakeFollowApprover) ApproveFollow(ctx context.Context, followerID, followeeID string) (string, error) {
	_ = ctx
	f.followerID = followerID
	f.followeeID = followeeID
	return "ok: follow accepted delivery enqueued", nil
}

func TestCommandHandlerSubscribesFollowApprove(t *testing.T) {
	source := &fakeCommandSource{}
	approver := &fakeFollowApprover{}
	unsubscribe, err := NewCommandHandler(source, approver).Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	if source.name != CommandFollowApprove {
		t.Fatalf("subscription name = %q", source.name)
	}
	if source.handle == nil {
		t.Fatalf("subscription handler was not stored")
	}
	source.handle(CommandMessage{
		Name: CommandFollowApprove,
		Data: map[string]any{
			"follower_id": "remote-alice",
			"followee_id": "relay",
		},
	})
	if approver.followerID != "remote-alice" || approver.followeeID != "relay" {
		t.Fatalf("approval call = follower:%q followee:%q", approver.followerID, approver.followeeID)
	}
	unsubscribe()
	if source.handle != nil {
		t.Fatalf("unsubscribe did not clear handler")
	}
}

func TestCommandHandlerRejectsUnknownCommand(t *testing.T) {
	err := NewCommandHandler(nil, &fakeFollowApprover{}).Handle(context.Background(), CommandMessage{Name: "unknown.command"})
	if err == nil {
		t.Fatalf("expected unknown command to fail")
	}
}

func TestCommandHandlerRequiresFollowIDs(t *testing.T) {
	err := NewCommandHandler(nil, &fakeFollowApprover{}).Handle(context.Background(), CommandMessage{
		Name: CommandFollowApprove,
		Data: map[string]any{"follower_id": "remote-alice"},
	})
	if err == nil {
		t.Fatalf("expected missing followee_id to fail")
	}
}
