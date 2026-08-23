package types

import (
	"errors"
	"fmt"
)

type Object map[string]any

var ValidPostTypes = map[string]struct{}{
	"Note":     {},
	"Question": {},
	"Article":  {},
	"Audio":    {},
	"Document": {},
	"Image":    {},
	"Page":     {},
	"Video":    {},
	"Event":    {},
}

var ValidActorTypes = map[string]struct{}{
	"Person":       {},
	"Service":      {},
	"Group":        {},
	"Organization": {},
	"Application":  {},
}

func ToArray(value any) []any {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []any:
		return v
	case []string:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return []any{value}
	}
}

func GetAPID(value any) (string, error) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return "", fmt.Errorf("activitypub id is empty")
		}
		return v, nil
	case map[string]any:
		return getStringField(v, "id", "cannot determine activitypub id")
	case Object:
		return getStringField(map[string]any(v), "id", "cannot determine activitypub id")
	default:
		return "", fmt.Errorf("cannot determine activitypub id from %T", value)
	}
}

func GetOneAPID(value any) (string, error) {
	items := ToArray(value)
	if len(items) == 0 {
		return "", fmt.Errorf("cannot determine activitypub id from empty value")
	}
	return GetAPID(items[0])
}

func GetAPIDs(value any) []string {
	items := ToArray(value)
	ids := make([]string, 0, len(items))
	for _, item := range items {
		id, err := GetAPID(item)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func GetAPType(value any) (string, error) {
	obj, ok := asMap(value)
	if !ok {
		return "", fmt.Errorf("cannot determine activitypub type from %T", value)
	}
	t, ok := obj["type"]
	if !ok {
		return "", fmt.Errorf("cannot determine activitypub type")
	}
	switch v := t.(type) {
	case string:
		if v == "" {
			return "", fmt.Errorf("activitypub type is empty")
		}
		return v, nil
	case []any:
		if len(v) == 0 {
			return "", fmt.Errorf("activitypub type array is empty")
		}
		first, ok := v[0].(string)
		if !ok || first == "" {
			return "", fmt.Errorf("activitypub type array does not start with a string")
		}
		return first, nil
	case []string:
		if len(v) == 0 || v[0] == "" {
			return "", fmt.Errorf("activitypub type array is empty")
		}
		return v[0], nil
	default:
		return "", fmt.Errorf("cannot determine activitypub type from %T", t)
	}
}

func IsType(value any, typ string) bool {
	got, err := GetAPType(value)
	return err == nil && got == typ
}

func IsCreate(value any) bool {
	return IsType(value, "Create")
}

func IsDelete(value any) bool {
	return IsType(value, "Delete")
}

func IsUpdate(value any) bool {
	return IsType(value, "Update")
}

func IsFollow(value any) bool {
	return IsType(value, "Follow")
}

func IsAccept(value any) bool {
	return IsType(value, "Accept")
}

func IsReject(value any) bool {
	return IsType(value, "Reject")
}

func IsUndo(value any) bool {
	return IsType(value, "Undo")
}

func IsAnnounce(value any) bool {
	return IsType(value, "Announce")
}

func IsLike(value any) bool {
	typ, err := GetAPType(value)
	if err != nil {
		return false
	}
	return typ == "Like" || typ == "EmojiReaction" || typ == "EmojiReact"
}

func IsBlock(value any) bool {
	return IsType(value, "Block")
}

func IsFlag(value any) bool {
	return IsType(value, "Flag")
}

func IsMove(value any) bool {
	return IsType(value, "Move")
}

func IsPost(value any) bool {
	typ, err := GetAPType(value)
	if err != nil {
		return false
	}
	_, ok := ValidPostTypes[typ]
	return ok
}

func IsActor(value any) bool {
	typ, err := GetAPType(value)
	if err != nil {
		return false
	}
	_, ok := ValidActorTypes[typ]
	return ok
}

func IsCollection(value any) bool {
	return IsType(value, "Collection")
}

func IsOrderedCollection(value any) bool {
	return IsType(value, "OrderedCollection")
}

func IsCollectionOrOrderedCollection(value any) bool {
	return IsCollection(value) || IsOrderedCollection(value)
}

func asMap(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return v, true
	case Object:
		return map[string]any(v), true
	default:
		return nil, false
	}
}

func getStringField(obj map[string]any, field, message string) (string, error) {
	v, ok := obj[field]
	if !ok {
		return "", errors.New(message)
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", errors.New(message)
	}
	return s, nil
}
