package notes

import (
	"net/url"
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
		"tag":        renderTags(note),
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

func renderTags(note *domainnotes.Note) []any {
	tags := make([]any, 0, len(note.Hashtags)+len(note.MentionURIs)+len(note.Emojis))
	for _, tag := range note.Hashtags {
		tags = append(tags, map[string]any{
			"type": "Hashtag",
			"href": "/tags/" + url.PathEscape(tag),
			"name": "#" + tag,
		})
	}
	for _, mention := range note.MentionURIs {
		tags = append(tags, map[string]any{
			"type": "Mention",
			"href": mention,
		})
	}
	for _, emoji := range note.Emojis {
		name := emoji.Name
		if name == "" || emoji.IconURL == "" {
			continue
		}
		updated := time.Now().UTC().Format(time.RFC3339)
		if emoji.UpdatedAt != nil {
			updated = emoji.UpdatedAt.UTC().Format(time.RFC3339)
		}
		mediaType := emoji.MediaType
		if mediaType == "" {
			mediaType = "image/png"
		}
		id := emoji.URI
		if id == "" {
			id = "/emojis/" + url.PathEscape(name)
		}
		tags = append(tags, map[string]any{
			"id":      id,
			"type":    "Emoji",
			"name":    ":" + name + ":",
			"updated": updated,
			"icon": map[string]any{
				"type":      "Image",
				"mediaType": mediaType,
				"url":       emoji.IconURL,
			},
		})
	}
	return tags
}
