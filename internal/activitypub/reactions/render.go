package reactions

import (
	"net/url"
	"strings"

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
