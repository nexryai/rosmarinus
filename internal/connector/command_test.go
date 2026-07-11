package connector

import (
	"context"
	"testing"
)

type fakeCommandSource struct {
	names   []string
	handles map[string]func(CommandMessage)
}

func (f *fakeCommandSource) Subscribe(ctx context.Context, name string, handle func(CommandMessage)) (func(), error) {
	_ = ctx
	if f.handles == nil {
		f.handles = map[string]func(CommandMessage){}
	}
	f.names = append(f.names, name)
	f.handles[name] = handle
	return func() { delete(f.handles, name) }, nil
}

type fakeFollowApprover struct {
	approvedFollowerID string
	approvedFolloweeID string
	rejectedFollowerID string
	rejectedFolloweeID string
}

func (f *fakeFollowApprover) ApproveFollow(ctx context.Context, followerID, followeeID string) (string, error) {
	_ = ctx
	f.approvedFollowerID = followerID
	f.approvedFolloweeID = followeeID
	return "ok: follow accepted delivery enqueued", nil
}

func (f *fakeFollowApprover) RejectFollow(ctx context.Context, followerID, followeeID string) (string, error) {
	_ = ctx
	f.rejectedFollowerID = followerID
	f.rejectedFolloweeID = followeeID
	return "ok: follow rejected delivery enqueued", nil
}

func TestCommandHandlerSubscribesFollowDecisionCommands(t *testing.T) {
	source := &fakeCommandSource{}
	approver := &fakeFollowApprover{}
	unsubscribe, err := NewCommandHandler(source, approver).Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	if len(source.names) != 2 || source.names[0] != CommandFollowApprove || source.names[1] != CommandFollowReject {
		t.Fatalf("subscription names = %+v", source.names)
	}
	if source.handles[CommandFollowApprove] == nil || source.handles[CommandFollowReject] == nil {
		t.Fatalf("subscription handler was not stored")
	}
	source.handles[CommandFollowApprove](CommandMessage{
		Name: CommandFollowApprove,
		Data: map[string]any{
			"follower_id": "remote-alice",
			"followee_id": "relay",
		},
	})
	if approver.approvedFollowerID != "remote-alice" || approver.approvedFolloweeID != "relay" {
		t.Fatalf("approval call = follower:%q followee:%q", approver.approvedFollowerID, approver.approvedFolloweeID)
	}
	source.handles[CommandFollowReject](CommandMessage{
		Name: CommandFollowReject,
		Data: map[string]any{
			"follower_id": "remote-bob",
			"followee_id": "relay",
		},
	})
	if approver.rejectedFollowerID != "remote-bob" || approver.rejectedFolloweeID != "relay" {
		t.Fatalf("rejection call = follower:%q followee:%q", approver.rejectedFollowerID, approver.rejectedFolloweeID)
	}
	unsubscribe()
	if len(source.handles) != 0 {
		t.Fatalf("unsubscribe did not clear handlers: %+v", source.handles)
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
