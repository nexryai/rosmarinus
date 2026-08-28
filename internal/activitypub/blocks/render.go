package blocks

import (
	"net/url"
	"strings"
	"time"

	domainblocks "github.com/nexryai/rosmarinus/internal/domain/blocks"
)

func RenderBlock(publicURL string, block *domainblocks.Block) map[string]any {
	return map[string]any{
		"@context": "https://www.w3.org/ns/activitystreams",
		"id":       strings.TrimRight(publicURL, "/") + "/blocks/" + url.PathEscape(block.ID),
		"type":     "Block",
		"actor":    block.BlockerURI,
		"object":   block.BlockeeURI,
	}
}

func RenderUndoBlock(publicURL string, block *domainblocks.Block, published time.Time) map[string]any {
	activity := RenderBlock(publicURL, block)
	delete(activity, "@context")
	return map[string]any{
		"@context":  "https://www.w3.org/ns/activitystreams",
		"id":        activity["id"].(string) + "/undo",
		"type":      "Undo",
		"actor":     block.BlockerURI,
		"object":    activity,
		"published": published.UTC().Format(time.RFC3339),
	}
}
