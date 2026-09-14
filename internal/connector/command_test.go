package connector

import (
	"context"
	"testing"
	"time"
)

type recordingExecutor struct {
	post  PostCreateCommand
	mute  MuteCreateCommand
	actor ActorCreateCommand
}

func (e *recordingExecutor) CreateFollow(context.Context, string, string) (string, error) {
	return "follow-1", nil
}
func (e *recordingExecutor) DeleteFollow(context.Context, FollowDeleteCommand) (FollowDeleted, error) {
	return FollowDeleted{}, nil
}
func (e *recordingExecutor) ApproveFollow(context.Context, string, string) (string, error) {
	return "follow-1", nil
}
func (e *recordingExecutor) RejectFollow(context.Context, string, string) (string, error) {
	return "follow-1", nil
}
func (e *recordingExecutor) RejectAndBlockFollow(context.Context, string, string) (BlockCreated, error) {
	return BlockCreated{}, nil
}
func (e *recordingExecutor) CreatePost(_ context.Context, command PostCreateCommand) (PostCreated, error) {
	e.post = command
	return PostCreated{NoteID: command.NoteID}, nil
}
func (e *recordingExecutor) DeletePost(context.Context, PostDeleteCommand) (PostDeleted, error) {
	return PostDeleted{}, nil
}
func (e *recordingExecutor) VotePoll(context.Context, PollVoteCommand) (PollVoted, error) {
	return PollVoted{}, nil
}
func (e *recordingExecutor) CreateReaction(context.Context, ReactionCreateCommand) (ReactionCreated, error) {
	return ReactionCreated{}, nil
}
func (e *recordingExecutor) DeleteReaction(context.Context, ReactionDeleteCommand) (ReactionDeleted, error) {
	return ReactionDeleted{}, nil
}
func (e *recordingExecutor) CreateBlock(context.Context, BlockCreateCommand) (BlockCreated, error) {
	return BlockCreated{}, nil
}
func (e *recordingExecutor) DeleteBlock(context.Context, BlockDeleteCommand) (BlockDeleted, error) {
	return BlockDeleted{}, nil
}
func (e *recordingExecutor) CreateMute(_ context.Context, command MuteCreateCommand) (MuteCreated, error) {
	e.mute = command
	return MuteCreated{MuteID: "mute-1", MuteeID: "remote-1", ExpiresAt: command.ExpiresAt}, nil
}
func (e *recordingExecutor) DeleteMute(context.Context, MuteDeleteCommand) (MuteDeleted, error) {
	return MuteDeleted{}, nil
}
func (e *recordingExecutor) CreateActor(_ context.Context, _ string, command ActorCreateCommand) (ActorCreated, error) {
	e.actor = command
	return ActorCreated{ActorID: "actor-1"}, nil
}
func (e *recordingExecutor) UpdateActor(context.Context, string, ActorUpdateCommand) (ActorUpdated, error) {
	return ActorUpdated{}, nil
}
func (e *recordingExecutor) DeleteActor(context.Context, string, ActorDeleteCommand) (ActorDeleted, error) {
	return ActorDeleted{}, nil
}
func (e *recordingExecutor) MarkNotificationRead(context.Context, string, string, string) (NotificationRead, error) {
	return NotificationRead{}, nil
}
func (e *recordingExecutor) MarkAllNotificationsRead(context.Context, string, string) (NotificationsRead, error) {
	return NotificationsRead{}, nil
}

func TestExecuteCommandBuildsPostCommand(t *testing.T) {
	executor := &recordingExecutor{}
	_, actorID, err := ExecuteCommand(context.Background(), executor, CommandPostCreate, "account-1", "actor-1", PostCreateData{
		Text: "hello", EmojiNames: []string{"salvia"}, MediaIDs: []string{"media-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if actorID != "actor-1" || executor.post.ActorID != "actor-1" || executor.post.NoteID != "" || executor.post.Text != "hello" || len(executor.post.EmojiNames) != 1 || len(executor.post.MediaIDs) != 1 {
		t.Fatalf("post command = %+v actor=%q", executor.post, actorID)
	}
}

func TestExecuteCommandRejectsRenoteWithContent(t *testing.T) {
	_, _, err := ExecuteCommand(context.Background(), &recordingExecutor{}, CommandPostCreate, "account-1", "actor-1", PostCreateData{
		RenoteID: "target-1", Text: "not allowed",
	})
	if err == nil {
		t.Fatal("expected mixed renote and content to be rejected")
	}
}

func TestExecuteCommandBuildsMuteCommand(t *testing.T) {
	executor := &recordingExecutor{}
	expiresAt := time.Now().UTC().Add(time.Hour)
	_, actorID, err := ExecuteCommand(context.Background(), executor, CommandMuteCreate, "account-1", "actor-1", MuteCreateData{Target: "https://remote.test/users/bob", ExpiresAt: &expiresAt})
	if err != nil {
		t.Fatal(err)
	}
	if actorID != "actor-1" || executor.mute.ActorID != "actor-1" || executor.mute.Target != "https://remote.test/users/bob" || executor.mute.ExpiresAt == nil || !executor.mute.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("mute command = %+v actor=%q", executor.mute, actorID)
	}
}

func TestExecuteCommandReturnsCreatedActorIdentity(t *testing.T) {
	executor := &recordingExecutor{}
	_, actorID, err := ExecuteCommand(context.Background(), executor, CommandActorCreate, "account-1", "", ActorCreateData{Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if actorID != "actor-1" || executor.actor.Username != "alice" {
		t.Fatalf("actor command = %+v result actor=%q", executor.actor, actorID)
	}
}

func TestExecuteCommandRejectsUnknownOperation(t *testing.T) {
	if _, _, err := ExecuteCommand(context.Background(), &recordingExecutor{}, "unknown", "account-1", "actor-1", map[string]any{}); err == nil {
		t.Fatal("expected unknown operation to fail")
	}
}
