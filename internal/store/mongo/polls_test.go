package mongostore

import (
	"testing"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/polls"
)

func TestPollDocumentPreservesQuestionState(t *testing.T) {
	expiresAt := time.Date(2026, time.August, 25, 0, 0, 0, 0, time.UTC)
	doc := fromPoll(polls.Poll{
		NoteID: "note-1", AuthorID: "actor-1", Choices: []string{"cats", "dogs"},
		Votes: []int{2, 3}, Multiple: true, ExpiresAt: &expiresAt,
	})
	if doc.NoteID != "note-1" || doc.AuthorID != "actor-1" || !doc.Multiple || len(doc.Votes) != 2 || doc.ExpiresAt == nil {
		t.Fatalf("unexpected poll document: %+v", doc)
	}
}

func TestPollVoteScopeEnforcesSingleAndMultipleChoiceUniqueness(t *testing.T) {
	if pollVoteScope("note", "actor", 0, false) != pollVoteScope("note", "actor", 1, false) {
		t.Fatal("single-choice votes must share a scope")
	}
	if pollVoteScope("note", "actor", 0, true) == pollVoteScope("note", "actor", 1, true) {
		t.Fatal("multiple-choice votes must have distinct scopes")
	}
}
