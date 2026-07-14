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
	approvedFollowerID string
	approvedFolloweeID string
	rejectedFollowerID string
	rejectedFolloweeID string
	postCommand        PostCreateCommand
	postCalls          int
	err                error
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

func (f *fakeReceiptStore) Complete(ctx context.Context, accountID, requestID string, result any, now time.Time) error {
	_ = ctx
	receipt := f.receipts[accountID+"\x00"+requestID]
	receipt.Status = CommandReceiptCompleted
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
	if len(source.names) != 3 || source.names[0] != CommandFollowApprove || source.names[1] != CommandFollowReject || source.names[2] != CommandPostCreate {
		t.Fatalf("subscription names = %+v", source.names)
	}
	unsubscribe()
	if len(source.handles) != 0 {
		t.Fatalf("unsubscribe did not clear handlers: %+v", source.handles)
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
