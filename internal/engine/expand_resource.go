package engine

import (
	"context"
	"encoding/json"
	"fmt"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
)

// resourceSource is the ResourceSource FilePartSource variant
// (message-v2 / openapi ResourceSource).
type resourceSource struct {
	Type       string             `json:"type"`
	ClientName string             `json:"clientName"`
	URI        string             `json:"uri"`
	Text       filePartSourceText `json:"text"`
}

const maxMCPResourceBlobBytes = 10 * 1024 * 1024

var supportedMCPResourceAttachmentMIMEs = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// expandPromptParts expands client-supplied resource file parts into synthetic
// text (and optional media) parts, mirroring opencode SessionPrompt.resolveUserPart
// for source.type === "resource" (prompt.ts:703-783). Non-resource parts pass
// through unchanged.
func (e *Engine) expandPromptParts(ctx context.Context, parts []PartInput) []PartInput {
	out := make([]PartInput, 0, len(parts))
	for _, p := range parts {
		if p.Type == "file" && len(p.Source) > 0 {
			var tag struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(p.Source, &tag) == nil && tag.Type == "resource" {
				out = append(out, e.expandResourcePart(ctx, p)...)
				continue
			}
		}
		out = append(out, p)
	}
	return out
}

func (e *Engine) expandResourcePart(ctx context.Context, part PartInput) []PartInput {
	var src resourceSource
	if err := json.Unmarshal(part.Source, &src); err != nil || src.ClientName == "" || src.URI == "" {
		return []PartInput{part}
	}
	pieces := []PartInput{{
		Type:      "text",
		Text:      fmt.Sprintf("Reading MCP resource: %s (%s)", part.Filename, src.URI),
		Synthetic: true,
	}}
	if e.cfg.MCP == nil {
		pieces = append(pieces, PartInput{
			Type: "text", Text: fmt.Sprintf("Failed to read MCP resource %s: MCP unavailable", part.Filename),
			Synthetic: true,
		})
		return pieces
	}
	content, err := e.cfg.MCP.ReadResource(ctx, src.ClientName, src.URI)
	if err != nil {
		pieces = append(pieces, PartInput{
			Type: "text", Text: fmt.Sprintf("Failed to read MCP resource %s: %v", part.Filename, err),
			Synthetic: true,
		})
		return pieces
	}
	if content == nil {
		pieces = append(pieces, PartInput{
			Type: "text", Text: fmt.Sprintf("Failed to read MCP resource %s: resource not found", part.Filename),
			Synthetic: true,
		})
		return pieces
	}
	for _, c := range content.Contents {
		switch item := c.(type) {
		case mcpgo.TextResourceContents:
			if item.Text == "" {
				continue
			}
			pieces = append(pieces, PartInput{Type: "text", Text: item.Text, Synthetic: true})
		case *mcpgo.TextResourceContents:
			if item == nil || item.Text == "" {
				continue
			}
			pieces = append(pieces, PartInput{Type: "text", Text: item.Text, Synthetic: true})
		case mcpgo.BlobResourceContents:
			pieces = append(pieces, expandBlobResource(part, src.URI, item.MIMEType, item.URI, item.Blob)...)
		case *mcpgo.BlobResourceContents:
			if item == nil {
				continue
			}
			pieces = append(pieces, expandBlobResource(part, src.URI, item.MIMEType, item.URI, item.Blob)...)
		}
	}
	return pieces
}

func expandBlobResource(part PartInput, fallbackURI, mime, itemURI, blob string) []PartInput {
	if mime == "" {
		mime = part.MIME
	}
	filename := itemURI
	if filename == "" {
		filename = part.Filename
	}
	if filename == "" {
		filename = fallbackURI
	}
	size := mcpResourceBase64Size(blob)
	if !supportedMCPResourceAttachmentMIMEs[mime] {
		return []PartInput{{
			Type: "text", Synthetic: true,
			Text: fmt.Sprintf("[Binary MCP resource omitted: %s (%s, %s) is not a supported attachment type]",
				filename, mime, formatMCPResourceBytes(size)),
		}}
	}
	if size > maxMCPResourceBlobBytes {
		return []PartInput{{
			Type: "text", Synthetic: true,
			Text: fmt.Sprintf("[Binary MCP resource omitted: %s (%s, %s) exceeds %s]",
				filename, mime, formatMCPResourceBytes(size), formatMCPResourceBytes(maxMCPResourceBlobBytes)),
		}}
	}
	return []PartInput{
		{
			Type: "text", Synthetic: true,
			Text: fmt.Sprintf("[Binary MCP resource attached: %s (%s)]", filename, mime),
		},
		{
			Type:     "file",
			MIME:     mime,
			Filename: filename,
			URL:      "data:" + mime + ";base64," + blob,
		},
	}
}

// mcpResourceBase64Size estimates decoded byte length from a base64 payload
// (same approximation as opencode mcpResourceBase64Size).
func mcpResourceBase64Size(blob string) int {
	n := len(blob)
	pad := 0
	if n >= 1 && blob[n-1] == '=' {
		pad++
	}
	if n >= 2 && blob[n-2] == '=' {
		pad++
	}
	return (n*3)/4 - pad
}

func formatMCPResourceBytes(n int) string {
	const kib = 1024
	const mib = 1024 * kib
	switch {
	case n >= mib:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
