package blocks

import (
	"testing"
	"time"

	domainblocks "github.com/nexryai/rosmarinus/internal/domain/blocks"
)

func TestRenderBlock(t *testing.T) {
	block := &domainblocks.Block{
		ID:         "block/id",
		BlockerURI: "https://local.example/users/alice",
		BlockeeURI: "https://remote.example/users/bob",
	}
	rendered := RenderBlock("https://local.example/", block)
	if rendered["id"] != "https://local.example/blocks/block%2Fid" || rendered["type"] != "Block" || rendered["actor"] != block.BlockerURI || rendered["object"] != block.BlockeeURI {
		t.Fatalf("unexpected Block activity: %#v", rendered)
	}
}

func TestRenderUndoBlock(t *testing.T) {
	published := time.Date(2026, 8, 28, 1, 2, 3, 0, time.UTC)
	block := &domainblocks.Block{
		ID:         "block-id",
		BlockerURI: "https://local.example/users/alice",
		BlockeeURI: "https://remote.example/users/bob",
	}
	rendered := RenderUndoBlock("https://local.example", block, published)
	if rendered["id"] != "https://local.example/blocks/block-id/undo" || rendered["type"] != "Undo" || rendered["actor"] != block.BlockerURI || rendered["published"] != published.Format(time.RFC3339) {
		t.Fatalf("unexpected Undo activity: %#v", rendered)
	}
	object, ok := rendered["object"].(map[string]any)
	if !ok || object["id"] != "https://local.example/blocks/block-id" || object["type"] != "Block" || object["object"] != block.BlockeeURI {
		t.Fatalf("unexpected Undo object: %#v", rendered["object"])
	}
}
