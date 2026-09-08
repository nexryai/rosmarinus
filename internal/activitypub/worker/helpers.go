package worker

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	apsig "github.com/nexryai/rosmarinus/internal/activitypub/signature"
	aptypes "github.com/nexryai/rosmarinus/internal/activitypub/types"
	"github.com/nexryai/rosmarinus/internal/domain/actors"
)

func firstString(value any) string {
	items := aptypes.ToArray(value)
	if len(items) == 0 {
		return ""
	}
	if s, ok := items[0].(string); ok {
		return s
	}
	return ""
}

func isDeletePostType(typ string) bool {
	_, ok := aptypes.ValidPostTypes[typ]
	return ok
}

func reactionFromActivity(activity map[string]any) string {
	for _, field := range []string{"_misskey_reaction", "content", "name"} {
		if reaction := firstString(activity[field]); reaction != "" {
			return reaction
		}
	}
	return "like"
}

func flagComment(content string, objectURIs []string) string {
	raw, err := json.MarshalIndent(objectURIs, "", "  ")
	if err != nil {
		raw = []byte("[]")
	}
	return content + "\n" + string(raw)
}

func copyCreateAudience(activity map[string]any, object map[string]any) {
	to := uniqueValues(append(aptypes.ToArray(activity["to"]), aptypes.ToArray(object["to"])...))
	cc := uniqueValues(append(aptypes.ToArray(activity["cc"]), aptypes.ToArray(object["cc"])...))
	if len(to) > 0 {
		activity["to"] = to
		object["to"] = to
	}
	if len(cc) > 0 {
		activity["cc"] = cc
		object["cc"] = cc
	}
}

func uniqueValues(items []any) []any {
	seen := map[string]struct{}{}
	out := make([]any, 0, len(items))
	for _, item := range items {
		key := fmt.Sprintf("%v", item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func publishedAt(object map[string]any) *time.Time {
	raw, ok := object["published"].(string)
	if !ok || raw == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &t
}

func renderAccept(localActor *actors.Actor, follow map[string]any) map[string]any {
	return map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":     strings.TrimRight(localActor.URI, "/") + "#accepts/" + shortID(follow["id"]),
		"type":   "Accept",
		"actor":  localActor.URI,
		"object": follow,
	}
}

func renderReject(localActor *actors.Actor, follow map[string]any) map[string]any {
	return map[string]any{
		"@context": []any{
			"https://www.w3.org/ns/activitystreams",
			"https://w3id.org/security/v1",
		},
		"id":     strings.TrimRight(localActor.URI, "/") + "#rejects/" + shortID(follow["id"]),
		"type":   "Reject",
		"actor":  localActor.URI,
		"object": follow,
	}
}

func signatureFromPayload(payload map[string]any) (apsig.HTTPSignature, error) {
	keyID, _ := payload["keyId"].(string)
	algorithm, _ := payload["algorithm"].(string)
	signingString, _ := payload["signingString"].(string)
	rawSignature, _ := payload["signature"].(string)
	if keyID == "" || algorithm == "" || signingString == "" || rawSignature == "" {
		return apsig.HTTPSignature{}, fmt.Errorf("missing signature fields")
	}
	if !apsig.IsSupportedAlgorithm(algorithm) {
		return apsig.HTTPSignature{}, fmt.Errorf("unsupported signature algorithm: %s", algorithm)
	}
	decoded, err := base64.StdEncoding.DecodeString(rawSignature)
	if err != nil {
		return apsig.HTTPSignature{}, err
	}
	headers := stringsFromAny(payload["headers"])
	return apsig.HTTPSignature{
		KeyID:         keyID,
		Algorithm:     algorithm,
		Headers:       headers,
		Signature:     decoded,
		SigningString: signingString,
	}, nil
}

func stringsFromAny(value any) []string {
	if items, ok := value.([]string); ok {
		return items
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func hostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid url: %s", raw)
	}
	return strings.ToLower(u.Hostname()), nil
}

func shortID(value any) string {
	if s, ok := value.(string); ok && s != "" {
		parts := strings.Split(strings.TrimRight(s, "/"), "/")
		return parts[len(parts)-1]
	}
	return "activity"
}
