package polls

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	domainpolls "github.com/nexryai/rosmarinus/internal/domain/polls"
)

const maxChoices = 20
const maxChoiceLength = 256

func ParseQuestion(object map[string]any) (*domainpolls.Poll, error) {
	if !aptypes.IsType(object, "Question") {
		return nil, fmt.Errorf("object is not a Question")
	}
	rawChoices := object["oneOf"]
	multiple := false
	if rawChoices == nil {
		rawChoices = object["anyOf"]
		multiple = true
	}
	items := aptypes.ToArray(rawChoices)
	if len(items) == 0 || len(items) > maxChoices {
		return nil, fmt.Errorf("Question must contain between 1 and %d choices", maxChoices)
	}
	choices := make([]string, 0, len(items))
	votes := make([]int, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		choice, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Question choice is not an object")
		}
		name, ok := choice["name"].(string)
		name = strings.TrimSpace(name)
		if !ok || name == "" || utf8.RuneCountInString(name) > maxChoiceLength {
			return nil, fmt.Errorf("Question choice name is invalid")
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("Question contains duplicate choices")
		}
		seen[name] = struct{}{}
		count, err := choiceVotes(choice)
		if err != nil {
			return nil, err
		}
		choices = append(choices, name)
		votes = append(votes, count)
	}
	expiresAt, err := pollEnd(object)
	if err != nil {
		return nil, err
	}
	return &domainpolls.Poll{Choices: choices, Votes: votes, Multiple: multiple, ExpiresAt: expiresAt}, nil
}

func ParseUpdatedVotes(object map[string]any, choices []string) ([]int, error) {
	if !aptypes.IsType(object, "Question") {
		return nil, fmt.Errorf("object is not a Question")
	}
	rawChoices := object["oneOf"]
	if rawChoices == nil {
		rawChoices = object["anyOf"]
	}
	items := aptypes.ToArray(rawChoices)
	if len(items) == 0 {
		return nil, fmt.Errorf("Question choices are missing")
	}
	votes := make([]int, len(choices))
	for i, name := range choices {
		found := false
		for _, item := range items {
			choice, ok := item.(map[string]any)
			if !ok || choice["name"] != name {
				continue
			}
			replies, ok := choice["replies"].(map[string]any)
			if !ok || replies["totalItems"] == nil {
				return nil, fmt.Errorf("Question choice %q has no reply count", name)
			}
			count, err := nonnegativeInteger(replies["totalItems"])
			if err != nil {
				return nil, err
			}
			votes[i] = count
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("Question choice %q is missing", name)
		}
	}
	return votes, nil
}

func choiceVotes(choice map[string]any) (int, error) {
	value := choice["_misskey_votes"]
	if replies, ok := choice["replies"].(map[string]any); ok && replies["totalItems"] != nil {
		value = replies["totalItems"]
	}
	if value == nil {
		return 0, nil
	}
	return nonnegativeInteger(value)
}

func nonnegativeInteger(value any) (int, error) {
	switch count := value.(type) {
	case int:
		if count >= 0 {
			return count, nil
		}
	case int32:
		if count >= 0 {
			return int(count), nil
		}
	case int64:
		if count >= 0 && count <= int64(^uint(0)>>1) {
			return int(count), nil
		}
	case float64:
		if count >= 0 && count <= float64(math.MaxInt) && count == float64(int(count)) {
			return int(count), nil
		}
	}
	return 0, fmt.Errorf("Question vote count is invalid")
}

func pollEnd(object map[string]any) (*time.Time, error) {
	for _, key := range []string{"endTime", "closed"} {
		value, ok := object[key].(string)
		if !ok || value == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, fmt.Errorf("Question %s is invalid", key)
		}
		parsed = parsed.UTC()
		return &parsed, nil
	}
	return nil, nil
}
