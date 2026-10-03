package blocknote2md

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Option configures a conversion.
type Option func(*config)

type config struct {
	origin string
}

// WithOrigin sets the application origin (scheme and host, e.g.
// "https://paca.example.com"), the equivalent of the browser's
// window.location.origin. It is only used for blocks that export as a link to
// in-app content, such as Paca's annotation cards. Without it those links are
// relative paths.
func WithOrigin(origin string) Option {
	return func(c *config) { c.origin = origin }
}

// Convert renders a BlockNote document, a JSON array of blocks, as Markdown.
//
// An empty document ("", "null" or "[]") converts to "". Input that is not a
// JSON array is an error; anything inside the array that BlockNote itself would
// reject is skipped or degraded rather than failing the whole document.
func Convert(doc []byte, opts ...Option) (string, error) {
	trimmed := bytes.TrimSpace(doc)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return "", nil
	}
	var blocks []any
	if err := json.Unmarshal(trimmed, &blocks); err != nil {
		return "", fmt.Errorf("blocknote2md: document must be a JSON array of blocks: %w", err)
	}
	return ConvertBlocks(blocks, opts...), nil
}

// ConvertBlocks is Convert for a document that is already decoded with
// encoding/json (a []any of map[string]any blocks).
func ConvertBlocks(blocks []any, opts ...Option) string {
	var cfg config
	for _, o := range opts {
		o(&cfg)
	}
	conv := &blockConv{origin: cfg.origin}
	return hastToMarkdown(conv.blocksToHast(blocks))
}
