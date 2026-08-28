package actors

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nexryai/rosmarinus/internal/config"
	domainactors "github.com/nexryai/rosmarinus/internal/domain/actors"
	"github.com/nexryai/rosmarinus/internal/domain/emojis"
	"github.com/nexryai/rosmarinus/internal/mfm"
)

const publicAudience = "https://www.w3.org/ns/activitystreams#Public"

// RenderLocalActor renders the complete local Actor representation used by
// HTTP discovery and as the object in outbound Update(Person) activities.
// Identity and federation endpoints come from the persisted Actor; profile
// fields are rendered as ActivityPub profile properties.
func RenderLocalActor(cfg config.Config, actor *domainactors.Actor) map[string]any {
	return renderLocalActor(cfg, actor, nil, false)
}

func RenderLocalActorWithEmojis(cfg config.Config, actor *domainactors.Actor, localEmojis []emojis.Emoji) map[string]any {
	return renderLocalActor(cfg, actor, localEmojis, true)
}

func renderLocalActor(cfg config.Config, actor *domainactors.Actor, localEmojis []emojis.Emoji, resolvedEmojis bool) map[string]any {
	if actor == nil {
		return nil
	}
	base := strings.TrimRight(cfg.PublicURL, "/")
	actorURI := actor.URI
	if actorURI == "" {
		actorURI = base + "/users/" + url.PathEscape(actor.ID)
	}
	actorType := actor.Type
	if actorType == "" {
		actorType = "Service"
	}
	// Current Misskey represents user bots by switching Person and Service.
	// Preserve explicitly non-user and environment-provisioned Actor types.
	if !actor.IsSystemActor && actor.Type != "" && (actorType == "Person" || actorType == "Service") {
		if actor.IsBot {
			actorType = "Service"
		} else {
			actorType = "Person"
		}
	}
	inbox := actor.Inbox
	if inbox == "" {
		inbox = actorURI + "/inbox"
	}
	sharedInbox := actor.SharedInbox
	if sharedInbox == "" {
		sharedInbox = base + "/inbox"
	}
	outbox := actorURI + "/outbox"
	followers := actor.FollowersURI
	if followers == "" {
		followers = actorURI + "/followers"
	}
	following := actor.FollowingURI
	if following == "" {
		following = actorURI + "/following"
	}
	featured := actor.FeaturedURI
	if featured == "" {
		featured = actorURI + "/collections/featured"
	}

	profileSummary := any(nil)
	if actor.Summary != "" {
		profileSummary = mfm.ToHTML(actor.Summary, base).HTML
	}
	attachment := make([]any, 0, len(actor.ProfileFields))
	for _, field := range actor.ProfileFields {
		attachment = append(attachment, map[string]any{
			"type":  "PropertyValue",
			"name":  field.Name,
			"value": renderProfileFieldValue(field.Value),
		})
	}
	emojisByName := make(map[string]emojis.Emoji, len(localEmojis))
	for _, emoji := range localEmojis {
		emojisByName[emoji.Name] = emoji
	}
	tags := make([]any, 0, len(actor.EmojiNames)+len(actor.Tags))
	for _, name := range actor.EmojiNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tag := map[string]any{
			"id":   base + "/emojis/" + url.PathEscape(name),
			"type": "Emoji",
			"name": ":" + name + ":",
		}
		if resolvedEmojis {
			emoji, ok := emojisByName[name]
			if !ok {
				continue
			}
			iconURL := emoji.PublicURL
			if iconURL == "" {
				iconURL = emoji.OriginalURL
			}
			if iconURL == "" {
				continue
			}
			if emoji.URI != "" {
				tag["id"] = emoji.URI
			}
			mediaType := emoji.MediaType
			if mediaType == "" {
				mediaType = "image/png"
			}
			updatedAt := emoji.UpdatedAt
			if updatedAt.IsZero() {
				updatedAt = time.Now().UTC()
			}
			tag["updated"] = updatedAt.UTC().Format(time.RFC3339)
			tag["icon"] = map[string]any{
				"type": "Image", "mediaType": mediaType, "url": iconURL,
			}
		}
		tags = append(tags, tag)
	}
	for _, tag := range actor.Tags {
		tag = strings.TrimSpace(strings.TrimPrefix(tag, "#"))
		if tag == "" {
			continue
		}
		tags = append(tags, map[string]any{
			"type": "Hashtag",
			"href": base + "/tags/" + url.PathEscape(tag),
			"name": "#" + tag,
		})
	}

	icon := any(nil)
	if actor.AvatarURL != "" {
		icon = map[string]any{
			"type":      "Image",
			"url":       actor.AvatarURL,
			"sensitive": false,
			"name":      nil,
		}
	}
	image := any(nil)
	if actor.BannerURL != "" {
		image = map[string]any{
			"type":      "Image",
			"url":       actor.BannerURL,
			"sensitive": false,
			"name":      nil,
		}
	}

	alsoKnownAs := append([]string(nil), actor.AlsoKnownAs...)
	if alsoKnownAs == nil {
		alsoKnownAs = []string{}
	}
	publicKey := RenderPublicKey(actor)
	if actor.URI == "" {
		publicKey["owner"] = actorURI
		if actor.PublicKeyID == "" {
			publicKey["id"] = actorURI + "#main-key"
		}
	}
	profileURL := actor.URL
	if profileURL == "" {
		profileURL = base + "/@" + url.PathEscape(actor.Username)
	}
	discoverable := actor.IsDiscoverable
	if actor.IsSystemActor {
		// Existing environment-provisioned Actors predate the persisted flag and
		// were always advertised as discoverable.
		discoverable = true
	}
	body := map[string]any{
		"type":              actorType,
		"id":                actorURI,
		"inbox":             inbox,
		"outbox":            outbox,
		"followers":         followers,
		"following":         following,
		"featured":          featured,
		"sharedInbox":       sharedInbox,
		"endpoints":         map[string]any{"sharedInbox": sharedInbox},
		"url":               profileURL,
		"preferredUsername": actor.Username,
		"name":              actor.Name,
		"summary":           profileSummary,
		"_misskey_summary":  actor.Summary,
		// Rosmarinus requires explicit follow approval for local actors.
		"manuallyApprovesFollowers": true,
		"discoverable":              discoverable,
		"publicKey":                 publicKey,
		"alsoKnownAs":               alsoKnownAs,
		"attachment":                attachment,
		"tag":                       tags,
		"icon":                      icon,
		"image":                     image,
		"isCat":                     actor.IsCat,
	}
	if actor.Birthday != "" {
		body["vcard:bday"] = actor.Birthday
	}
	if actor.Location != "" {
		body["vcard:Address"] = actor.Location
	}
	if actor.MovedToURI != "" {
		body["movedTo"] = actor.MovedToURI
	}
	return withActivityContext(body)
}

// RenderActor and RenderPerson are descriptive aliases for callers that do
// not need to distinguish local Actor storage from the ActivityPub Person
// representation.
func RenderActor(cfg config.Config, actor *domainactors.Actor) map[string]any {
	return RenderLocalActor(cfg, actor)
}

func RenderPerson(cfg config.Config, actor *domainactors.Actor) map[string]any {
	return RenderLocalActor(cfg, actor)
}

// RenderUpdate renders a Misskey-compatible Update whose object is the full
// current Person representation. Call RenderUpdateAt when deterministic IDs or
// timestamps are needed by a caller or test.
func RenderUpdate(cfg config.Config, actor *domainactors.Actor) map[string]any {
	return RenderUpdateAt(cfg, actor, "", time.Now().UTC())
}

func RenderUpdateAt(cfg config.Config, actor *domainactors.Actor, activityID string, published time.Time) map[string]any {
	return renderUpdateAt(cfg, actor, nil, false, activityID, published)
}

func RenderUpdateAtWithEmojis(cfg config.Config, actor *domainactors.Actor, localEmojis []emojis.Emoji, activityID string, published time.Time) map[string]any {
	return renderUpdateAt(cfg, actor, localEmojis, true, activityID, published)
}

func renderUpdateAt(cfg config.Config, actor *domainactors.Actor, localEmojis []emojis.Emoji, resolvedEmojis bool, activityID string, published time.Time) map[string]any {
	if actor == nil {
		return nil
	}
	person := renderLocalActor(cfg, actor, localEmojis, resolvedEmojis)
	actorURI, _ := person["id"].(string)
	if published.IsZero() {
		published = time.Now().UTC()
	}
	if activityID == "" {
		activityID = actorURI + "#updates/" + strconv.FormatInt(published.UnixMilli(), 10)
	}
	return withActivityContext(map[string]any{
		"id":        activityID,
		"actor":     actorURI,
		"type":      "Update",
		"to":        []string{publicAudience},
		"object":    person,
		"published": published.UTC().Format(time.RFC3339Nano),
	})
}

func RenderUpdateWithID(cfg config.Config, actor *domainactors.Actor, activityID string, published time.Time) map[string]any {
	return RenderUpdateAt(cfg, actor, activityID, published)
}

func RenderPublicKey(actor *domainactors.Actor) map[string]any {
	if actor == nil {
		return nil
	}
	keyID := actor.PublicKeyID
	if keyID == "" && actor.URI != "" {
		keyID = actor.URI + "#main-key"
	}
	owner := actor.URI
	if owner == "" {
		owner = keyID
		if hash := strings.Index(owner, "#"); hash >= 0 {
			owner = owner[:hash]
		}
	}
	return withActivityContext(map[string]any{
		"id":           keyID,
		"type":         "Key",
		"owner":        owner,
		"publicKeyPem": actor.PublicKeyPEM,
	})
}

func renderProfileFieldValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			href := mfm.EscapeHTML(parsed.String())
			display := mfm.EscapeHTML(value)
			return `<a href="` + href + `" rel="me nofollow noopener" target="_blank">` + display + `</a>`
		}
	}
	return mfm.EscapeHTML(value)
}

func withActivityContext(body map[string]any) map[string]any {
	body["@context"] = []any{
		"https://www.w3.org/ns/activitystreams",
		"https://w3id.org/security/v1",
		map[string]any{
			"manuallyApprovesFollowers": "as:manuallyApprovesFollowers",
			"sensitive":                 "as:sensitive",
			"Hashtag":                   "as:Hashtag",
			"quoteUrl":                  "as:quoteUrl",
			"toot":                      "http://joinmastodon.org/ns#",
			"Emoji":                     "toot:Emoji",
			"featured":                  "toot:featured",
			"discoverable":              "toot:discoverable",
			"schema":                    "http://schema.org#",
			"PropertyValue":             "schema:PropertyValue",
			"value":                     "schema:value",
			"misskey":                   "https://misskey-hub.net/ns#",
			"_misskey_content":          "misskey:_misskey_content",
			"_misskey_quote":            "misskey:_misskey_quote",
			"_misskey_summary":          "misskey:_misskey_summary",
			"isCat":                     "misskey:isCat",
			"vcard":                     "http://www.w3.org/2006/vcard/ns#",
		},
	}
	return body
}
