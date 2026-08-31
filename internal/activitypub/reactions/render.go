package reactions

import (
	"net/url"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	domainreactions "github.com/nexryai/rosmarinus/internal/domain/reactions"
)

func RenderLike(publicURL string, reaction *domainreactions.Reaction) map[string]any {
	return RenderLikeWithEmoji(publicURL, reaction, nil)
}

func LocalEmojiName(reaction string) (string, bool) {
	if len(reaction) < 3 || reaction[0] != ':' || reaction[len(reaction)-1] != ':' {
		return "", false
	}
	name := reaction[1 : len(reaction)-1]
	if strings.HasSuffix(name, "@.") {
		return strings.TrimSuffix(name, "@."), true
	}
	if strings.Contains(name, "@") {
		return "", false
	}
	return name, true
}

func RenderLikeWithEmoji(publicURL string, reaction *domainreactions.Reaction, emoji *emojis.Emoji) map[string]any {
	activity := map[string]any{
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
	if tag := renderEmojiTag(publicURL, emoji); tag != nil {
		activity["tag"] = []any{tag}
	}
	return activity
}

func renderEmojiTag(publicURL string, emoji *emojis.Emoji) map[string]any {
	if emoji == nil || emoji.Name == "" {
		return nil
	}
	iconURL := emoji.PublicURL
	if iconURL == "" {
		iconURL = emoji.OriginalURL
	}
	if iconURL == "" {
		return nil
	}
	uri := emoji.URI
	if uri == "" {
		uri = strings.TrimRight(publicURL, "/") + "/emojis/" + url.PathEscape(emoji.Name)
	}
	mediaType := emoji.MediaType
	if mediaType == "" {
		mediaType = "image/png"
	}
	updatedAt := emoji.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = emoji.CreatedAt
	}
	tag := map[string]any{
		"id":   uri,
		"type": "Emoji",
		"name": ":" + emoji.Name + ":",
		"icon": map[string]any{
			"type":      "Image",
			"mediaType": mediaType,
			"url":       iconURL,
		},
	}
	if !updatedAt.IsZero() {
		tag["updated"] = updatedAt.UTC().Format(time.RFC3339)
	}
	return tag
}

func RenderUndoLikeWithEmoji(publicURL string, reaction *domainreactions.Reaction, emoji *emojis.Emoji, published time.Time) map[string]any {
	like := RenderLikeWithEmoji(publicURL, reaction, emoji)
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

func RenderUndoLike(publicURL string, reaction *domainreactions.Reaction, published time.Time) map[string]any {
	return RenderUndoLikeWithEmoji(publicURL, reaction, nil, published)
}
