package connector

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/account"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
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

type fakeAccountLookup struct {
	accounts map[string]*account.Account
}

func (f *fakeAccountLookup) FindActiveByAblyClientID(ctx context.Context, clientID string) (*account.Account, error) {
	_ = ctx
	return f.accounts[clientID], nil
}

type fakeActorOwnershipLookup struct {
	actors map[string]*actors.Actor
}

func (f *fakeActorOwnershipLookup) FindOwnedLocalByID(ctx context.Context, accountID, actorID string) (*actors.Actor, error) {
	_ = ctx
	return f.actors[accountID+"\x00"+actorID], nil
}

type fakeCommandExecutor struct {
	createdFollowerID   string
	createdTarget       string
	approvedFollowerID  string
	approvedFolloweeID  string
	rejectedFollowerID  string
	rejectedFolloweeID  string
	postCommand         PostCreateCommand
	postCalls           int
	reactionCommand     ReactionCreateCommand
	reactionCalls       int
	reactionDelete      ReactionDeleteCommand
	reactionDeleteCalls int
	err                 error
}

func (f *fakeCommandExecutor) CreateFollow(ctx context.Context, followerID, target string) (string, error) {
	_ = ctx
	f.createdFollowerID = followerID
	f.createdTarget = target
	return "created", f.err
}

func (f *fakeCommandExecutor) ApproveFollow(ctx context.Context, followerID, followeeID string) (string, error) {
	_ = ctx
	f.approvedFollowerID = followerID
	f.approvedFolloweeID = followeeID
	return "approved", f.err
}

func (f *fakeCommandExecutor) RejectFollow(ctx context.Context, followerID, followeeID string) (string, error) {
	_ = ctx
	f.rejectedFollowerID = followerID
	f.rejectedFolloweeID = followeeID
	return "rejected", f.err
}

func (f *fakeCommandExecutor) CreatePost(ctx context.Context, command PostCreateCommand) (PostCreated, error) {
	_ = ctx
	f.postCalls++
	f.postCommand = command
	return PostCreated{AccountID: "account-1", ActorID: command.ActorID, NoteID: command.NoteID, URI: "https://example.test/notes/" + command.NoteID}, f.err
}

func (f *fakeCommandExecutor) CreateReaction(ctx context.Context, command ReactionCreateCommand) (ReactionCreated, error) {
	_ = ctx
	f.reactionCalls++
	f.reactionCommand = command
	return ReactionCreated{
		ReactionID: "reaction-created",
		NoteID:     command.NoteID,
		Reaction:   command.Reaction,
		URI:        "https://example.test/likes/reaction-created",
	}, f.err
}

func (f *fakeCommandExecutor) DeleteReaction(ctx context.Context, command ReactionDeleteCommand) (ReactionDeleted, error) {
	_ = ctx
	f.reactionDeleteCalls++
	f.reactionDelete = command
	return ReactionDeleted{
		ReactionID: "reaction-created",
		NoteID:     command.NoteID,
		URI:        "https://example.test/likes/reaction-created/undo",
	}, f.err
}

func (f *fakeCommandExecutor) CreateActor(ctx context.Context, accountID string, command ActorCreateCommand) (ActorCreated, error) {
	_ = ctx
	return ActorCreated{ActorID: "actor-created", URI: "https://example.test/users/actor-created", Username: command.Username}, f.err
}

type publishedCommandResult struct {
	accountID string
	requestID string
	actorID   string
	command   string
	result    any
	code      string
}

type fakeCommandResultPublisher struct {
	succeeded []publishedCommandResult
	failed    []publishedCommandResult
	actors    []ActorCreated
}

func (f *fakeCommandResultPublisher) PublishActorCreated(ctx context.Context, accountID, requestID string, created ActorCreated) error {
	_ = ctx
	f.actors = append(f.actors, created)
	return nil
}

func (f *fakeCommandResultPublisher) PublishCommandSucceeded(ctx context.Context, accountID, requestID, actorID, command string, result any) error {
	_ = ctx
	f.succeeded = append(f.succeeded, publishedCommandResult{accountID: accountID, requestID: requestID, actorID: actorID, command: command, result: result})
	return nil
}

func (f *fakeCommandResultPublisher) PublishCommandFailed(ctx context.Context, accountID, requestID, actorID, command, code string) error {
	_ = ctx
	f.failed = append(f.failed, publishedCommandResult{accountID: accountID, requestID: requestID, actorID: actorID, command: command, code: code})
	return nil
}

type fakeReceiptStore struct {
	receipts map[string]*CommandReceipt
}

func (f *fakeReceiptStore) Claim(ctx context.Context, receipt CommandReceipt) (*CommandReceipt, bool, error) {
	_ = ctx
	if f.receipts == nil {
		f.receipts = map[string]*CommandReceipt{}
	}
	key := receipt.AccountID + "\x00" + receipt.RequestID
	if existing := f.receipts[key]; existing != nil {
		copy := *existing
		return &copy, false, nil
	}
	copy := receipt
	f.receipts[key] = &copy
	return &copy, true, nil
}

func (f *fakeReceiptStore) Complete(ctx context.Context, accountID, requestID, actorID string, result any, now time.Time) error {
	_ = ctx
	receipt := f.receipts[accountID+"\x00"+requestID]
	receipt.Status = CommandReceiptCompleted
	receipt.ActorID = actorID
	receipt.Result = result
	receipt.UpdatedAt = now
	return nil
}

func (f *fakeReceiptStore) Fail(ctx context.Context, accountID, requestID, code string, now time.Time) error {
	_ = ctx
	receipt := f.receipts[accountID+"\x00"+requestID]
	receipt.Status = CommandReceiptFailed
	receipt.ErrorCode = code
	receipt.UpdatedAt = now
	return nil
}

func newAuthorizedCommandHandler(executor *fakeCommandExecutor, publisher *fakeCommandResultPublisher, receipts CommandReceiptStore) *CommandHandler {
	return NewCommandHandler(nil,
		&fakeAccountLookup{accounts: map[string]*account.Account{
			"client-1": {ID: "account-1", AblyClientID: "client-1", Status: account.StatusActive},
		}},
		&fakeActorOwnershipLookup{actors: map[string]*actors.Actor{
			"account-1\x00actor-1": {ID: "actor-1", OwnerAccountID: "account-1"},
			"account-1\x00actor-2": {ID: "actor-2", OwnerAccountID: "account-1"},
		}},
		executor, publisher, receipts, nil, time.Hour,
	)
}

func commandMessage(name, requestID, actorID string, data any) CommandMessage {
	return CommandMessage{
		ID:       "ably-message-1",
		ClientID: "client-1",
		Name:     name,
		Data: CommandEnvelope{
			Version:   1,
			RequestID: requestID,
			ActorID:   actorID,
			Data:      data,
		},
	}
}

func TestCommandHandlerSubscribesCommands(t *testing.T) {
	source := &fakeCommandSource{}
	handler := newAuthorizedCommandHandler(&fakeCommandExecutor{}, &fakeCommandResultPublisher{}, nil)
	handler.source = source
	unsubscribe, err := handler.Subscribe(context.Background())
	if err != nil {
		t.Fatalf("Subscribe returned error: %v", err)
	}
	if len(source.names) != 7 || source.names[0] != CommandFollowCreate || source.names[1] != CommandFollowApprove || source.names[2] != CommandFollowReject || source.names[3] != CommandPostCreate || source.names[4] != CommandReactionCreate || source.names[5] != CommandReactionDelete || source.names[6] != CommandActorCreate {
		t.Fatalf("subscription names = %+v", source.names)
	}
	unsubscribe()
	if len(source.handles) != 0 {
		t.Fatalf("unsubscribe did not clear handlers: %+v", source.handles)
	}
}

func TestCommandHandlerCreatesReactionAsOwnedActor(t *testing.T) {
	executor := &fakeCommandExecutor{}
	publisher := &fakeCommandResultPublisher{}
	handler := newAuthorizedCommandHandler(executor, publisher, &fakeReceiptStore{})
	message := commandMessage(CommandReactionCreate, "request-reaction", "actor-2", ReactionCreateData{
		NoteID:   "remote-note",
		Reaction: "👍",
	})
	if err := handler.Handle(context.Background(), message); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if executor.reactionCalls != 1 || executor.reactionCommand.ActorID != "actor-2" || executor.reactionCommand.NoteID != "remote-note" || executor.reactionCommand.Reaction != "👍" {
		t.Fatalf("reaction command = %+v calls=%d", executor.reactionCommand, executor.reactionCalls)
	}
	if len(publisher.succeeded) != 1 || publisher.succeeded[0].command != CommandReactionCreate {
		t.Fatalf("unexpected command result: %+v", publisher.succeeded)
	}
}

func TestCommandHandlerDeletesReactionAsOwnedActor(t *testing.T) {
	executor := &fakeCommandExecutor{}
	publisher := &fakeCommandResultPublisher{}
	handler := newAuthorizedCommandHandler(executor, publisher, &fakeReceiptStore{})
	message := commandMessage(CommandReactionDelete, "request-reaction-delete", "actor-2", ReactionDeleteData{
		NoteID: "remote-note",
	})
	if err := handler.Handle(context.Background(), message); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if executor.reactionDeleteCalls != 1 || executor.reactionDelete.ActorID != "actor-2" || executor.reactionDelete.NoteID != "remote-note" {
		t.Fatalf("reaction delete command = %+v calls=%d", executor.reactionDelete, executor.reactionDeleteCalls)
	}
	if len(publisher.succeeded) != 1 || publisher.succeeded[0].command != CommandReactionDelete {
		t.Fatalf("unexpected command result: %+v", publisher.succeeded)
	}
}

func TestCommandHandlerCreatesFollowAsOwnedActor(t *testing.T) {
	executor := &fakeCommandExecutor{}
	handler := newAuthorizedCommandHandler(executor, &fakeCommandResultPublisher{}, &fakeReceiptStore{})
	message := commandMessage(CommandFollowCreate, "request-follow", "actor-2", FollowCreateData{Target: "alice@remote.example"})
	if err := handler.Handle(context.Background(), message); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if executor.createdFollowerID != "actor-2" || executor.createdTarget != "alice@remote.example" {
		t.Fatalf("follow command = actor:%q target:%q", executor.createdFollowerID, executor.createdTarget)
	}
}

func TestCommandHandlerAuthorizesMultipleOwnedActors(t *testing.T) {
	executor := &fakeCommandExecutor{}
	publisher := &fakeCommandResultPublisher{}
	handler := newAuthorizedCommandHandler(executor, publisher, &fakeReceiptStore{})

	for i, actorID := range []string{"actor-1", "actor-2"} {
		err := handler.Handle(context.Background(), commandMessage(CommandPostCreate, "request-"+actorID, actorID, PostCreateData{
			NoteID: "note-" + actorID,
			Text:   "hello",
		}))
		if err != nil {
			t.Fatalf("Handle actor %d returned error: %v", i, err)
		}
	}
	if executor.postCalls != 2 {
		t.Fatalf("post calls = %d", executor.postCalls)
	}
	if len(publisher.succeeded) != 2 || publisher.succeeded[1].actorID != "actor-2" {
		t.Fatalf("unexpected results: %+v", publisher.succeeded)
	}
}

func TestCommandHandlerRejectsCrossAccountActor(t *testing.T) {
	handler := newAuthorizedCommandHandler(&fakeCommandExecutor{}, &fakeCommandResultPublisher{}, nil)
	message := commandMessage(CommandPostCreate, "request-1", "other-actor", PostCreateData{NoteID: "note-1", Text: "hello"})
	if err := handler.Handle(context.Background(), message); err == nil {
		t.Fatal("expected cross-account actor to fail")
	}
}

func TestCommandHandlerRequiresIdentifiedClient(t *testing.T) {
	handler := newAuthorizedCommandHandler(&fakeCommandExecutor{}, &fakeCommandResultPublisher{}, nil)
	message := commandMessage(CommandPostCreate, "request-1", "actor-1", PostCreateData{NoteID: "note-1", Text: "hello"})
	message.ClientID = ""
	if err := handler.Handle(context.Background(), message); err == nil {
		t.Fatal("expected missing clientId to fail")
	}
}

func TestCommandHandlerUsesActorAsFollowee(t *testing.T) {
	executor := &fakeCommandExecutor{}
	handler := newAuthorizedCommandHandler(executor, &fakeCommandResultPublisher{}, &fakeReceiptStore{})
	if err := handler.Handle(context.Background(), commandMessage(CommandFollowApprove, "request-1", "actor-2", FollowApproveData{FollowerID: "remote-1"})); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if executor.approvedFollowerID != "remote-1" || executor.approvedFolloweeID != "actor-2" {
		t.Fatalf("approval = follower:%q followee:%q", executor.approvedFollowerID, executor.approvedFolloweeID)
	}
}

func TestCommandHandlerCreatesActorForAuthenticatedAccount(t *testing.T) {
	executor := &fakeCommandExecutor{}
	publisher := &fakeCommandResultPublisher{}
	receipts := &fakeReceiptStore{}
	handler := newAuthorizedCommandHandler(executor, publisher, receipts)
	message := commandMessage(CommandActorCreate, "request-create", "", ActorCreateData{Username: "alice-work", Type: "Person"})
	if err := handler.Handle(context.Background(), message); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if len(publisher.succeeded) != 1 || publisher.succeeded[0].actorID != "actor-created" {
		t.Fatalf("unexpected success: %+v", publisher.succeeded)
	}
	if len(publisher.actors) != 1 || publisher.actors[0].ActorID != "actor-created" {
		t.Fatalf("unexpected actor.created events: %+v", publisher.actors)
	}
	receipt := receipts.receipts["account-1\x00request-create"]
	if receipt == nil || receipt.ActorID != "actor-created" || receipt.Status != CommandReceiptCompleted {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
}

func TestCommandHandlerDeduplicatesRequestAndRepublishesResult(t *testing.T) {
	executor := &fakeCommandExecutor{}
	publisher := &fakeCommandResultPublisher{}
	handler := newAuthorizedCommandHandler(executor, publisher, &fakeReceiptStore{})
	message := commandMessage(CommandPostCreate, "request-1", "actor-1", PostCreateData{NoteID: "note-1", Text: "hello"})
	if err := handler.Handle(context.Background(), message); err != nil {
		t.Fatalf("first Handle returned error: %v", err)
	}
	if err := handler.Handle(context.Background(), message); err != nil {
		t.Fatalf("second Handle returned error: %v", err)
	}
	if executor.postCalls != 1 {
		t.Fatalf("post calls = %d", executor.postCalls)
	}
	if len(publisher.succeeded) != 2 {
		t.Fatalf("success publications = %d", len(publisher.succeeded))
	}
}

func TestCommandReceiptDeduplicatesAcrossHandlers(t *testing.T) {
	executor := &fakeCommandExecutor{}
	publisher := &fakeCommandResultPublisher{}
	receipts := &fakeReceiptStore{}
	first := newAuthorizedCommandHandler(executor, publisher, receipts)
	second := newAuthorizedCommandHandler(executor, publisher, receipts)
	message := commandMessage(CommandPostCreate, "shared-request", "actor-1", PostCreateData{NoteID: "note-1", Text: "hello"})
	if err := first.Handle(context.Background(), message); err != nil {
		t.Fatalf("first handler returned error: %v", err)
	}
	if err := second.Handle(context.Background(), message); err != nil {
		t.Fatalf("second handler returned error: %v", err)
	}
	if executor.postCalls != 1 {
		t.Fatalf("post calls = %d", executor.postCalls)
	}
}

func TestCommandHandlerRejectsSuspendedAccount(t *testing.T) {
	handler := newAuthorizedCommandHandler(&fakeCommandExecutor{}, &fakeCommandResultPublisher{}, nil)
	lookup := handler.accounts.(*fakeAccountLookup)
	lookup.accounts["client-1"].Status = account.StatusSuspended
	message := commandMessage(CommandPostCreate, "request-1", "actor-1", PostCreateData{NoteID: "note-1", Text: "hello"})
	if err := handler.Handle(context.Background(), message); err == nil {
		t.Fatal("expected suspended account to fail")
	}
}

func TestCommandHandlerRejectsDeletedMissingAndRotatedClients(t *testing.T) {
	handler := newAuthorizedCommandHandler(&fakeCommandExecutor{}, &fakeCommandResultPublisher{}, nil)
	lookup := handler.accounts.(*fakeAccountLookup)
	lookup.accounts["deleted-client"] = &account.Account{ID: "deleted", AblyClientID: "deleted-client", Status: account.StatusDeleted}
	lookup.accounts["new-client"] = &account.Account{ID: "account-1", AblyClientID: "new-client", Status: account.StatusActive}

	for _, clientID := range []string{"deleted-client", "missing-client", "old-client"} {
		message := commandMessage(CommandPostCreate, "request-"+clientID, "actor-1", PostCreateData{NoteID: "note-1", Text: "hello"})
		message.ClientID = clientID
		if err := handler.Handle(context.Background(), message); err == nil {
			t.Fatalf("expected client %q to fail", clientID)
		}
	}
	message := commandMessage(CommandPostCreate, "request-new-client", "actor-1", PostCreateData{NoteID: "note-1", Text: "hello"})
	message.ClientID = "new-client"
	if err := handler.Handle(context.Background(), message); err != nil {
		t.Fatalf("rotated client returned error: %v", err)
	}
}

func TestCommandHandlerRecordsAndPublishesFailure(t *testing.T) {
	executor := &fakeCommandExecutor{err: errors.New("boom")}
	publisher := &fakeCommandResultPublisher{}
	receipts := &fakeReceiptStore{}
	handler := newAuthorizedCommandHandler(executor, publisher, receipts)
	message := commandMessage(CommandPostCreate, "request-1", "actor-1", PostCreateData{NoteID: "note-1", Text: "hello"})
	if err := handler.Handle(context.Background(), message); err == nil {
		t.Fatal("expected execution error")
	}
	receipt := receipts.receipts["account-1\x00request-1"]
	if receipt == nil || receipt.Status != CommandReceiptFailed || receipt.ErrorCode != "command_failed" {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	if len(publisher.failed) != 1 || publisher.failed[0].code != "command_failed" {
		t.Fatalf("unexpected failure publications: %+v", publisher.failed)
	}
}

func TestCommandHandlerRejectsUnsupportedEnvelopeVersion(t *testing.T) {
	handler := newAuthorizedCommandHandler(&fakeCommandExecutor{}, &fakeCommandResultPublisher{}, nil)
	message := commandMessage(CommandPostCreate, "request-1", "actor-1", PostCreateData{NoteID: "note-1", Text: "hello"})
	envelope := message.Data.(CommandEnvelope)
	envelope.Version = 2
	message.Data = envelope
	if err := handler.Handle(context.Background(), message); err == nil {
		t.Fatal("expected unsupported version to fail")
	}
}
