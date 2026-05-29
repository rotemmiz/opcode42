package message

import (
	"encoding/json"
	"fmt"
	"strings"
)

// isMedia reports whether a MIME type is image or PDF (util/media.ts:6-8).
func isMedia(mime string) bool {
	return strings.HasPrefix(mime, "image/") || mime == "application/pdf"
}

func fileName(name string) string {
	if name == "" {
		return "file"
	}
	return name
}

func hasNonSpace(s string) bool { return strings.TrimSpace(s) != "" }

// signature returns the Anthropic reasoning signature if present
// (metadata.anthropic.signature), used to detect signed reasoning.
func signature(metadata map[string]any) any {
	anth, ok := metadata["anthropic"].(map[string]any)
	if !ok {
		return nil
	}
	return anth["signature"]
}

// providerMeta strips the providerExecuted key and returns nil if nothing else
// remains (message-v2.ts:624-628).
func providerMeta(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	rest := make(map[string]any, len(metadata))
	for k, v := range metadata {
		if k == "providerExecuted" {
			continue
		}
		rest[k] = v
	}
	if len(rest) == 0 {
		return nil
	}
	return rest
}

// truncateToolOutput caps tool output for compaction (message-v2.ts:281-285).
// It counts runes (not bytes) so a cut never lands mid-rune and corrupts the
// wire with a replacement char; opencode counts UTF-16 units, so astral-plane
// chars differ by a hair, but the BMP-common case matches exactly.
func truncateToolOutput(text string, maxChars int) string {
	if maxChars <= 0 {
		return text
	}
	r := []rune(text)
	if len(r) <= maxChars {
		return text
	}
	omitted := len(r) - maxChars
	return fmt.Sprintf("%s\n[Tool output truncated for compaction: omitted %d chars]", string(r[:maxChars]), omitted)
}

// decodeState unmarshals a tool state into the given typed variant.
func decodeState(raw json.RawMessage, dst any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}
