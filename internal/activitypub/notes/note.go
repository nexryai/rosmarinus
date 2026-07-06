package notes

import (
	"fmt"
	"net/url"
	"strings"

	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
)

const PublicAudience = "https://www.w3.org/ns/activitystreams#Public"

type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityHome      Visibility = "home"
	VisibilityFollowers Visibility = "followers"
	VisibilitySpecified Visibility = "specified"
)

type Note struct {
	URI          string
	AttributedTo string
	Text         string
	Visibility   Visibility
}

func ParseRemoteNote(object map[string]any, entryURI string) (*Note, error) {
	if err := ValidateNote(object, entryURI); err != nil {
		return nil, err
	}
	id, err := aptypes.GetAPID(object)
	if err != nil {
		return nil, fmt.Errorf("note must have an id: %w", err)
	}
	idURL, err := url.Parse(id)
	if err != nil || idURL.Scheme != "https" {
		return nil, fmt.Errorf("unexpected schema of note.id: %s", id)
	}
	actorID, err := aptypes.GetOneAPID(object["attributedTo"])
	if err != nil {
		return nil, fmt.Errorf("note attributedTo is required: %w", err)
	}
	if hostOf(id) != hostOf(actorID) {
		return nil, fmt.Errorf("note id host doesn't match actor host")
	}
	return &Note{
		URI:          id,
		AttributedTo: actorID,
		Text:         noteText(object),
		Visibility:   ParseVisibility(actorID, object["to"], object["cc"]),
	}, nil
}

func ValidateNote(object map[string]any, entryURI string) error {
	if object == nil {
		return fmt.Errorf("invalid Note: object is null")
	}
	if !aptypes.IsPost(object) {
		typ, _ := aptypes.GetAPType(object)
		return fmt.Errorf("invalid Note: invalid object type %s", typ)
	}
	expectHost := hostOf(entryURI)
	if id, ok := object["id"].(string); ok && id != "" && hostOf(id) != expectHost {
		return fmt.Errorf("invalid Note: id has different host. expected: %s, actual: %s", expectHost, hostOf(id))
	}
	if actorID, err := aptypes.GetOneAPID(object["attributedTo"]); err == nil && hostOf(actorID) != expectHost {
		return fmt.Errorf("invalid Note: attributedTo has different host. expected: %s, actual: %s", expectHost, hostOf(actorID))
	}
	return nil
}

func ParseVisibility(actorURI string, to, cc any) Visibility {
	toIDs := aptypes.GetAPIDs(to)
	ccIDs := aptypes.GetAPIDs(cc)
	if containsPublic(toIDs) {
		return VisibilityPublic
	}
	if containsPublic(ccIDs) {
		return VisibilityHome
	}
	followersURI := strings.TrimRight(actorURI, "/") + "/followers"
	if contains(toIDs, followersURI) || contains(ccIDs, followersURI) {
		return VisibilityFollowers
	}
	return VisibilitySpecified
}

func noteText(object map[string]any) string {
	if source, ok := object["source"].(map[string]any); ok {
		if source["mediaType"] == "text/x.misskeymarkdown" {
			if content, ok := source["content"].(string); ok {
				return content
			}
		}
	}
	if content, ok := object["_misskey_content"].(string); ok {
		return content
	}
	if content, ok := object["content"].(string); ok {
		return content
	}
	return ""
}

func containsPublic(ids []string) bool {
	for _, id := range ids {
		if id == PublicAudience || id == "as:Public" || id == "Public" {
			return true
		}
	}
	return false
}

func contains(ids []string, target string) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}
