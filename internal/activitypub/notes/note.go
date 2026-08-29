package notes

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	domainnotes "github.com/nexryai/rosmarinus/internal/domain/notes"
	"github.com/nexryai/rosmarinus/internal/mfm"
)

const PublicAudience = "https://www.w3.org/ns/activitystreams#Public"

const maxRemoteNoteMentions = 20
const maxRemoteNoteEmojis = 100

var earliestSafePublishedAt = time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)

type Visibility string

const (
	VisibilityPublic    Visibility = "public"
	VisibilityHome      Visibility = "home"
	VisibilityFollowers Visibility = "followers"
	VisibilitySpecified Visibility = "specified"
)

type Note struct {
	URI             string
	AttributedTo    string
	Text            string
	ContentWarning  *string
	Sensitive       bool
	InReplyToURI    string
	QuoteURI        string
	QuoteURIs       []string
	Visibility      Visibility
	MentionURIs     []string
	VisibleUserURIs []string
	Hashtags        []string
	Emojis          []domainnotes.Emoji
	Attachments     []domainnotes.Attachment
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
	visibility, audienceURIs := ParseAudience(actorID, object["to"], object["cc"])
	visibleUserURIs := []string(nil)
	if visibility == VisibilitySpecified {
		visibleUserURIs = audienceURIs
	}
	quoteURIs := extractQuoteURIs(object)
	quoteURI := ""
	if len(quoteURIs) > 0 {
		quoteURI = quoteURIs[0]
	}
	return &Note{
		URI:             id,
		AttributedTo:    actorID,
		Text:            noteText(object),
		ContentWarning:  contentWarning(object),
		Sensitive:       boolField(object, "sensitive"),
		InReplyToURI:    optionalAPID(object["inReplyTo"]),
		QuoteURI:        quoteURI,
		QuoteURIs:       quoteURIs,
		Visibility:      visibility,
		MentionURIs:     mergeUnique(ExtractMentionURIs(object["tag"]), audienceURIs),
		VisibleUserURIs: visibleUserURIs,
		Hashtags:        ExtractHashtags(object["tag"]),
		Emojis:          ExtractEmojis(object["tag"]),
		Attachments:     ExtractAttachments(object["attachment"], boolField(object, "sensitive")),
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
	if rawPublished, exists := object["published"]; exists && rawPublished != nil && rawPublished != "" {
		published, ok := rawPublished.(string)
		if !ok {
			return fmt.Errorf("invalid Note: published timestamp is malformed")
		}
		parsed, err := time.Parse(time.RFC3339, published)
		if err != nil || parsed.Before(earliestSafePublishedAt) {
			return fmt.Errorf("invalid Note: published timestamp is malformed")
		}
	}
	if rawURL, exists := object["url"]; exists && rawURL != nil {
		noteURL := firstAPHref(rawURL)
		if noteURL == "" || !isHTTPSURL(noteURL) {
			return fmt.Errorf("unexpected schema of note url: %s", noteURL)
		}
	}
	actorID, _ := aptypes.GetOneAPID(object["attributedTo"])
	_, audienceURIs := ParseAudience(actorID, object["to"], object["cc"])
	if len(mergeUnique(ExtractMentionURIs(object["tag"]), audienceURIs)) > maxRemoteNoteMentions {
		return fmt.Errorf("invalid Note: too many mentions")
	}
	return nil
}

func firstAPHref(value any) string {
	items := aptypes.ToArray(value)
	if len(items) == 0 {
		return ""
	}
	switch first := items[0].(type) {
	case string:
		return first
	case map[string]any:
		href, _ := first["href"].(string)
		return href
	default:
		return ""
	}
}

func isHTTPSURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}

func ParseVisibility(actorURI string, to, cc any) Visibility {
	visibility, _ := ParseAudience(actorURI, to, cc)
	return visibility
}

func ParseAudience(actorURI string, to, cc any) (Visibility, []string) {
	toIDs := aptypes.GetAPIDs(to)
	ccIDs := aptypes.GetAPIDs(cc)
	audienceURIs := directAudienceURIs(actorURI, toIDs, ccIDs)
	if containsPublic(toIDs) {
		return VisibilityPublic, audienceURIs
	}
	if containsPublic(ccIDs) {
		return VisibilityHome, audienceURIs
	}
	followersURI := strings.TrimRight(actorURI, "/") + "/followers"
	if contains(toIDs, followersURI) || contains(ccIDs, followersURI) {
		return VisibilityFollowers, audienceURIs
	}
	return VisibilitySpecified, audienceURIs
}

func directAudienceURIs(actorURI string, groups ...[]string) []string {
	followersURI := strings.TrimRight(actorURI, "/") + "/followers"
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, ids := range groups {
		for _, id := range ids {
			if id == "" || containsPublic([]string{id}) || id == followersURI {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

func mergeUnique(groups ...[]string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, group := range groups {
		for _, value := range group {
			if value == "" {
				continue
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	return out
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
		converted, err := mfm.FromHTML(content, hashtagNames(object["tag"]))
		if err == nil {
			return converted
		}
		return content
	}
	return ""
}

func hashtagNames(tags any) []string {
	items := aptypes.ToArray(tags)
	names := make([]string, 0, len(items))
	for _, item := range items {
		tag, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(tag, "Hashtag") {
			continue
		}
		name, ok := tag["name"].(string)
		if ok && name != "" {
			names = append(names, name)
		}
	}
	return names
}

func contentWarning(object map[string]any) *string {
	summary, ok := object["summary"].(string)
	if !ok || summary == "" {
		return nil
	}
	return &summary
}

func boolField(object map[string]any, field string) bool {
	v, ok := object[field].(bool)
	return ok && v
}

func optionalAPID(value any) string {
	if value == nil {
		return ""
	}
	id, err := aptypes.GetOneAPID(value)
	if err != nil {
		return ""
	}
	return id
}

func extractQuoteURIs(object map[string]any) []string {
	seen := make(map[string]struct{}, 2)
	result := make([]string, 0, 2)
	for _, key := range []string{"_misskey_quote", "quoteUrl"} {
		if uri, ok := object[key].(string); ok && uri != "" {
			if _, exists := seen[uri]; exists {
				continue
			}
			seen[uri] = struct{}{}
			result = append(result, uri)
		}
	}
	return result
}

func ExtractMentionURIs(tags any) []string {
	items := aptypes.ToArray(tags)
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		tag, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(tag, "Mention") {
			continue
		}
		href, ok := tag["href"].(string)
		if !ok || href == "" {
			continue
		}
		if _, ok := seen[href]; ok {
			continue
		}
		seen[href] = struct{}{}
		out = append(out, href)
	}
	return out
}

func ExtractHashtags(tags any) []string {
	items := aptypes.ToArray(tags)
	out := make([]string, 0, len(items))
	for _, item := range items {
		tag, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(tag, "Hashtag") {
			continue
		}
		name, ok := tag["name"].(string)
		if !ok || !strings.HasPrefix(name, "#") || len(name) < 2 {
			continue
		}
		out = append(out, name[1:])
	}
	return out
}

func ExtractEmojis(tags any) []domainnotes.Emoji {
	items := aptypes.ToArray(tags)
	out := make([]domainnotes.Emoji, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		if len(out) >= maxRemoteNoteEmojis {
			break
		}
		tag, ok := item.(map[string]any)
		if !ok || !aptypes.IsType(tag, "Emoji") {
			continue
		}
		icon, ok := tag["icon"].(map[string]any)
		if !ok {
			continue
		}
		iconURL, ok := icon["url"].(string)
		if !ok || len(iconURL) > 512 || !isHTTPSURL(iconURL) {
			continue
		}
		name, ok := tag["name"].(string)
		name = normalizeEmojiName(name)
		if !ok || name == "" || utf8.RuneCountInString(name) > 128 {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		emoji := domainnotes.Emoji{
			Name:      name,
			IconURL:   iconURL,
			MediaType: "image/png",
		}
		if id, ok := tag["id"].(string); ok && len(id) <= 512 && isHTTPSURL(id) {
			emoji.URI = id
		}
		if mediaType, ok := icon["mediaType"].(string); ok && mediaType != "" && len(mediaType) <= 64 {
			emoji.MediaType = mediaType
		}
		if updated, ok := tag["updated"].(string); ok && updated != "" {
			if t, err := time.Parse(time.RFC3339, updated); err == nil {
				emoji.UpdatedAt = &t
			}
		}
		out = append(out, emoji)
	}
	return out
}

func ExtractAttachments(value any, noteSensitive bool) []domainnotes.Attachment {
	items := aptypes.ToArray(value)
	out := make([]domainnotes.Attachment, 0, len(items))
	for _, item := range items {
		attachment, ok := item.(map[string]any)
		if !ok {
			continue
		}
		url := attachmentURL(attachment["url"])
		if url == "" {
			continue
		}
		typ, _ := aptypes.GetAPType(attachment)
		if typ == "" {
			typ = "Document"
		}
		mediaType, _ := attachment["mediaType"].(string)
		name, _ := attachment["name"].(string)
		id, _ := attachment["id"].(string)
		out = append(out, domainnotes.Attachment{
			URI:       id,
			Type:      typ,
			MediaType: mediaType,
			URL:       url,
			Name:      name,
			Sensitive: noteSensitive || boolField(attachment, "sensitive"),
		})
	}
	return out
}

func attachmentURL(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		if href, ok := v["href"].(string); ok {
			return href
		}
		if urlValue, ok := v["url"].(string); ok {
			return urlValue
		}
	case []any:
		for _, item := range v {
			if url := attachmentURL(item); url != "" {
				return url
			}
		}
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

func normalizeEmojiName(name string) string {
	return strings.ReplaceAll(strings.TrimSpace(name), ":", "")
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
