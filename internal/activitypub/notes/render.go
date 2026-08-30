package notes

import (
	"net/url"
	"strings"
	"time"

	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/domain/polls"
	"github.com/nexryai/rosmarinus/internal/mfm"
)

func Render(note *domainnotes.Note) map[string]any {
	return RenderWithPoll(note, nil)
}

func RenderWithPoll(note *domainnotes.Note, poll *polls.Poll) map[string]any {
	if note.RenoteURI != "" && note.Text == "" && note.InReplyToURI == "" && len(note.Attachments) == 0 {
		return RenderAnnounce(note)
	}
	to, cc := renderAudience(note.AttributedTo, note.Visibility, note.MentionURIs, note.VisibleUserURIs)
	published := note.CreatedAt
	if note.PublishedAt != nil {
		published = *note.PublishedAt
	}
	summary := any(nil)
	if note.ContentWarning != nil {
		summary = *note.ContentWarning
	}
	inReplyTo := any(nil)
	if note.InReplyToURI != "" {
		inReplyTo = note.InReplyToURI
	}
	quote := any(nil)
	if note.QuoteURI != "" {
		quote = note.QuoteURI
	}
	publicURL := publicOrigin(note.AttributedTo)
	rendered := mfm.ToHTML(note.Text, publicURL)
	if note.QuoteURI != "" {
		quotedURI := mfm.EscapeHTML(note.QuoteURI)
		rendered.HTML += `<br><br><span class="quote-inline">RE: <a href="` + quotedURI + `">` + quotedURI + `</a></span>`
		rendered.Advanced = true
	}
	body := map[string]any{
		"id":             note.URI,
		"type":           "Note",
		"attributedTo":   note.AttributedTo,
		"summary":        summary,
		"content":        rendered.HTML,
		"_misskey_quote": quote,
		"quoteUrl":       quote,
		"published":      published.UTC().Format(time.RFC3339),
		"to":             to,
		"cc":             cc,
		"inReplyTo":      inReplyTo,
		"attachment":     renderAttachments(note.Attachments),
		"sensitive":      note.Sensitive || note.ContentWarning != nil || hasSensitiveAttachment(note.Attachments),
		"tag":            renderTags(note, publicURL),
	}
	if rendered.Advanced {
		body["_misskey_content"] = note.Text
		body["source"] = map[string]any{
			"content":   note.Text,
			"mediaType": "text/x.misskeymarkdown",
		}
	}
	if poll != nil {
		body["type"] = "Question"
		key := "oneOf"
		if poll.Multiple {
			key = "anyOf"
		}
		choices := make([]any, 0, len(poll.Choices))
		for i, name := range poll.Choices {
			votes := 0
			if i < len(poll.Votes) {
				votes = poll.Votes[i]
			}
			choices = append(choices, map[string]any{
				"type": "Note", "name": name,
				"replies": map[string]any{"type": "Collection", "totalItems": votes},
			})
		}
		body[key] = choices
		if poll.ExpiresAt != nil {
			endKey := "endTime"
			if !poll.ExpiresAt.After(time.Now().UTC()) {
				endKey = "closed"
			}
			body[endKey] = poll.ExpiresAt.UTC().Format(time.RFC3339)
		}
	}
	return withContext(body)
}

func RenderAnnounce(note *domainnotes.Note) map[string]any {
	return renderAnnounce(note, note.URI)
}

// RenderLocalAnnounce uses the same dereferenceable activity ID as current
// Misskey while the local Note keeps its stable /notes/{id} application URI.
func RenderLocalAnnounce(note *domainnotes.Note) map[string]any {
	return renderAnnounce(note, note.URI+"/activity")
}

func renderAnnounce(note *domainnotes.Note, activityID string) map[string]any {
	to, cc := renderAudience(note.AttributedTo, note.Visibility, note.MentionURIs, note.VisibleUserURIs)
	published := note.CreatedAt
	if note.PublishedAt != nil {
		published = *note.PublishedAt
	}
	return withContext(map[string]any{
		"id":        activityID,
		"type":      "Announce",
		"actor":     note.AttributedTo,
		"published": published.UTC().Format(time.RFC3339),
		"to":        to,
		"cc":        cc,
		"object":    note.RenoteURI,
	})
}

func RenderUndoAnnounce(note *domainnotes.Note, deletedAt time.Time) map[string]any {
	announce := RenderLocalAnnounce(note)
	delete(announce, "@context")
	activityID, _ := announce["id"].(string)
	return withContext(map[string]any{
		"id":        activityID + "/undo",
		"type":      "Undo",
		"actor":     note.AttributedTo,
		"object":    announce,
		"published": deletedAt.UTC().Format(time.RFC3339),
	})
}

func RenderCreate(note *domainnotes.Note) map[string]any {
	return RenderCreateWithPoll(note, nil)
}

func RenderCreateWithPoll(note *domainnotes.Note, poll *polls.Poll) map[string]any {
	object := RenderWithPoll(note, poll)
	contextValue := object["@context"]
	delete(object, "@context")
	published := note.CreatedAt
	if note.PublishedAt != nil {
		published = *note.PublishedAt
	}
	return map[string]any{
		"@context":  contextValue,
		"id":        note.URI + "/activity",
		"type":      "Create",
		"actor":     note.AttributedTo,
		"published": published.UTC().Format(time.RFC3339),
		"to":        object["to"],
		"cc":        object["cc"],
		"object":    object,
	}
}

func RenderQuestionUpdate(note *domainnotes.Note, poll *polls.Poll, updatedAt time.Time) map[string]any {
	object := RenderWithPoll(note, poll)
	delete(object, "@context")
	return withContext(map[string]any{
		"id":        note.AttributedTo + "#updates/" + updatedAt.UTC().Format("20060102150405.000000000"),
		"actor":     note.AttributedTo,
		"type":      "Update",
		"to":        []string{PublicAudience},
		"object":    object,
		"published": updatedAt.UTC().Format(time.RFC3339),
	})
}

func RenderDelete(note *domainnotes.Note, deletedAt time.Time) map[string]any {
	return withContext(map[string]any{
		// Current Misskey rejects inbound activities without an ID even though its
		// own Delete renderer omits one. Keep this stable so retries are idempotent.
		"id":        note.URI + "#delete",
		"type":      "Delete",
		"actor":     note.AttributedTo,
		"object":    map[string]any{"id": note.URI, "type": "Tombstone"},
		"published": deletedAt.UTC().Format(time.RFC3339),
	})
}

func renderAudience(actorURI string, visibility domainnotes.Visibility, mentionURIs, visibleUserURIs []string) ([]string, []string) {
	followers := actorURI + "/followers"
	switch visibility {
	case domainnotes.VisibilityPublic:
		return []string{PublicAudience}, []string{followers}
	case domainnotes.VisibilityHome:
		return []string{followers}, []string{PublicAudience}
	case domainnotes.VisibilityFollowers:
		return []string{followers}, []string{}
	case domainnotes.VisibilitySpecified:
		if len(visibleUserURIs) > 0 {
			return visibleUserURIs, []string{}
		}
		return mentionURIs, []string{}
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

func renderTags(note *domainnotes.Note, publicURL string) []any {
	tags := make([]any, 0, len(note.Hashtags)+len(note.MentionURIs)+len(note.Emojis))
	for _, tag := range note.Hashtags {
		tags = append(tags, map[string]any{
			"type": "Hashtag",
			"href": publicURL + "/tags/" + url.PathEscape(tag),
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
			id = publicURL + "/emojis/" + url.PathEscape(name)
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

func publicOrigin(actorURI string) string {
	parsed, err := url.Parse(actorURI)
	if err == nil && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.Host != "" {
		return parsed.Scheme + "://" + parsed.Host
	}
	return strings.TrimRight(actorURI, "/")
}

func renderAttachments(attachments []domainnotes.Attachment) []any {
	if len(attachments) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(attachments))
	for _, attachment := range attachments {
		if attachment.URL == "" {
			continue
		}
		typ := attachment.Type
		if typ == "" {
			typ = "Document"
		}
		document := map[string]any{
			"type":      typ,
			"mediaType": attachment.MediaType,
			"url":       attachment.URL,
			"name":      attachment.Name,
			"sensitive": attachment.Sensitive,
		}
		if attachment.Width > 0 {
			document["width"] = attachment.Width
		}
		if attachment.Height > 0 {
			document["height"] = attachment.Height
		}
		out = append(out, document)
	}
	return out
}

func hasSensitiveAttachment(attachments []domainnotes.Attachment) bool {
	for _, attachment := range attachments {
		if attachment.Sensitive {
			return true
		}
	}
	return false
}
