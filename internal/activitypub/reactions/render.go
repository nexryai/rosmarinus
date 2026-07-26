package reactions

import (
	"net/url"
	"strings"
	"time"

	domainreactions "github.com/nexryai/rosmarinus/internal/domain/reactions"
)

func RenderLike(publicURL string, reaction *domainreactions.Reaction) map[string]any {
	return map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":                strings.TrimRight(publicURL, "/") + "/likes/" + url.PathEscape(reaction.ID),
		"type":              "Like",
		"actor":             reaction.ActorURI,
		"object":            reaction.NoteURI,
		"content":           reaction.Reaction,
		"_misskey_reaction": reaction.Reaction,
	}
}

func RenderUndoLike(publicURL string, reaction *domainreactions.Reaction, published time.Time) map[string]any {
	like := RenderLike(publicURL, reaction)
	delete(like, "@context")
	return map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":        like["id"].(string) + "/undo",
		"type":      "Undo",
		"actor":     reaction.ActorURI,
		"object":    like,
		"published": published.UTC().Format(time.RFC3339),
	}
}
