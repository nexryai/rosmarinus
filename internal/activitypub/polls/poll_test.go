package polls

import "testing"

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
