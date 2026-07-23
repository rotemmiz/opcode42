package engine

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExpandPromptParts_ResourceWithoutMCP(t *testing.T) {
	e := &Engine{cfg: Config{}}
	src, _ := json.Marshal(resourceSource{
		Type: "resource", ClientName: "srv", URI: "mcp://srv/docs",
		Text: filePartSourceText{Value: "@docs"},
	})
	got := e.expandPromptParts(context.Background(), []PartInput{
		{Type: "text", Text: "see @docs"},
		{Type: "file", Filename: "docs", MIME: "text/plain", URL: "mcp://srv/docs", Source: src},
	})
	// Original text part is preserved; resource file becomes synthetic header + failure.
	if len(got) != 3 {
		t.Fatalf("got %d parts (%+v), want text + header + failure", len(got), got)
	}
	if got[0].Type != "text" || got[0].Text != "see @docs" || got[0].Synthetic {
		t.Fatalf("original text = %+v", got[0])
	}
	if !got[1].Synthetic || got[1].Type != "text" {
		t.Fatalf("header = %+v", got[1])
	}
	if !got[2].Synthetic || got[2].Type != "text" {
		t.Fatalf("failure = %+v", got[2])
	}
}

func TestExpandBlobResource_Unsupported(t *testing.T) {
	got := expandBlobResource(PartInput{Filename: "bin", MIME: "application/octet-stream"},
		"mcp://x", "application/octet-stream", "", "AAAA")
	if len(got) != 1 || got[0].Type != "text" || !got[0].Synthetic {
		t.Fatalf("got %+v", got)
	}
}

func TestExpandBlobResource_Image(t *testing.T) {
	got := expandBlobResource(PartInput{Filename: "pic", MIME: "image/png"},
		"mcp://x", "image/png", "pic.png", "AAAA")
	if len(got) != 2 {
		t.Fatalf("got %d parts, want header+file", len(got))
	}
	if got[1].Type != "file" || got[1].URL != "data:image/png;base64,AAAA" {
		t.Fatalf("file part = %+v", got[1])
	}
}
