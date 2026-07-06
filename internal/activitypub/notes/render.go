package notes

import (
	"time"

	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
)

func Render(note *domainnotes.Note) map[string]any {
	to, cc := renderAudience(note.AttributedTo, note.Visibility)
	published := note.CreatedAt
	if note.PublishedAt != nil {
		published = *note.PublishedAt
	}
	return withContext(map[string]any{
		"id":               note.URI,
		"type":             "Note",
		"attributedTo":     note.AttributedTo,
		"summary":          nil,
		"content":          note.Text,
		"_misskey_content": note.Text,
		"source": map[string]any{
			"content":   note.Text,
			"mediaType": "text/x.misskeymarkdown",
		},
		"published":  published.UTC().Format(time.RFC3339),
		"to":         to,
		"cc":         cc,
		"inReplyTo":  nil,
		"attachment": []any{},
		"sensitive":  false,
		"tag":        []any{},
	})
}

func renderAudience(actorURI string, visibility domainnotes.Visibility) ([]string, []string) {
	followers := actorURI + "/followers"
	switch visibility {
	case domainnotes.VisibilityPublic:
		return []string{PublicAudience}, []string{followers}
	case domainnotes.VisibilityHome:
		return []string{followers}, []string{PublicAudience}
	case domainnotes.VisibilityFollowers:
		return []string{followers}, []string{}
	default:
		return []string{}, []string{}
	}
}

func withContext(body map[string]any) map[string]any {
	body["@context"] = []any{
		"https://www.w3.org/ns/activitystreams",
		map[string]any{
			"misskey":          "https://misskey-hub.net/ns#",
			"_misskey_content": "misskey:_misskey_content",
			"_misskey_quote":   "misskey:_misskey_quote",
		},
	}
	return body
}
