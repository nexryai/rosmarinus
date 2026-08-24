package polls

import (
	"testing"
	"time"

	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	domainpolls "github.com/nexryai/rosmarinus/internal/domain/polls"
)

func TestParseQuestionSupportsMultipleChoiceAndClosed(t *testing.T) {
	poll, err := ParseQuestion(map[string]any{
		"type": "Question", "closed": "2026-08-24T12:00:00Z",
		"anyOf": []any{
			map[string]any{"name": "one", "replies": map[string]any{"totalItems": float64(2)}},
			map[string]any{"name": "two"},
		},
	})
	if err != nil {
		t.Fatalf("ParseQuestion returned error: %v", err)
	}
	if !poll.Multiple || poll.ExpiresAt == nil || len(poll.Votes) != 2 || poll.Votes[0] != 2 || poll.Votes[1] != 0 {
		t.Fatalf("unexpected poll: %+v", poll)
	}
}

func TestParseQuestionRejectsInvalidVotesAndDuplicateChoices(t *testing.T) {
	for _, object := range []map[string]any{
		{"type": "Question", "oneOf": []any{map[string]any{"name": "one", "replies": map[string]any{"totalItems": -1}}}},
		{"type": "Question", "oneOf": []any{map[string]any{"name": "one"}, map[string]any{"name": "one"}}},
	} {
		if _, err := ParseQuestion(object); err == nil {
			t.Fatalf("ParseQuestion accepted invalid object: %#v", object)
		}
	}
}

func TestParseUpdatedVotesMatchesStoredChoicesAndRequiresReplies(t *testing.T) {
	votes, err := ParseUpdatedVotes(map[string]any{
		"type": "Question",
		"oneOf": []any{
			map[string]any{"name": "dogs", "replies": map[string]any{"totalItems": 4}},
			map[string]any{"name": "cats", "replies": map[string]any{"totalItems": 3}},
			map[string]any{"name": "ignored", "replies": map[string]any{"totalItems": 8}},
		},
	}, []string{"cats", "dogs"})
	if err != nil || len(votes) != 2 || votes[0] != 3 || votes[1] != 4 {
		t.Fatalf("votes=%#v err=%v", votes, err)
	}
	if _, err := ParseUpdatedVotes(map[string]any{
		"type":  "Question",
		"oneOf": []any{map[string]any{"name": "cats", "_misskey_votes": 3}},
	}, []string{"cats"}); err == nil {
		t.Fatal("accepted an update without replies.totalItems")
	}
}

func TestNewLocalPollUsesCurrentMisskeyLimits(t *testing.T) {
	expiresAt := time.Now().UTC().Add(time.Hour)
	poll, err := NewLocalPoll([]string{" cats ", "dogs"}, true, &expiresAt)
	if err != nil {
		t.Fatalf("NewLocalPoll returned error: %v", err)
	}
	if !poll.Multiple || poll.Choices[0] != "cats" || len(poll.Votes) != 2 || poll.ExpiresAt == nil {
		t.Fatalf("unexpected poll: %+v", poll)
	}
	if _, err := NewLocalPoll([]string{"one"}, false, nil); err == nil {
		t.Fatal("accepted a one-choice local poll")
	}
	if _, err := NewLocalPoll([]string{"same", "same"}, false, nil); err == nil {
		t.Fatal("accepted duplicate local poll choices")
	}
}

func TestRenderVoteUsesMisskeyReplyNoteShape(t *testing.T) {
	now := time.Date(2026, 8, 24, 1, 2, 3, 0, time.UTC)
	note := &domainnotes.Note{URI: "https://remote.example/notes/poll", AttributedTo: "https://remote.example/users/alice"}
	poll := &domainpolls.Poll{Choices: []string{"cats", "dogs"}}
	vote := &domainpolls.Vote{ID: "vote-1", Choice: 1}
	rendered := RenderVote("https://local.example/users/bob", vote, note, poll, now)
	object, ok := rendered["object"].(map[string]any)
	if rendered["type"] != "Create" || !ok || object["name"] != "dogs" || object["inReplyTo"] != note.URI {
		t.Fatalf("unexpected vote activity: %#v", rendered)
	}
}
