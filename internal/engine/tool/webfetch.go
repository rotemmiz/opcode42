package tool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// webfetchLimit caps how many bytes of a response webfetch returns.
const webfetchLimit = 100_000

// WebFetch fetches a URL and returns its (optionally de-HTMLed) content.
type WebFetch struct {
	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// Info describes the webfetch tool.
func (WebFetch) Info() Info {
	return Info{
		ID:          "webfetch",
		Description: "Fetch the contents of a URL. HTML is reduced to readable text.",
		Parameters: obj(map[string]any{
			"url":    strProp("The URL to fetch (http/https)."),
			"format": strProp("Optional: 'text' (default) reduces HTML; 'raw' returns the body verbatim."),
		}, "url"),
	}
}

type webfetchParams struct {
	URL    string `json:"url"`
	Format string `json:"format"`
}

// Run performs the GET and returns the body, HTML-stripped unless format=raw.
func (w WebFetch) Run(ctx context.Context, input map[string]any, _ Context) (Result, error) {
	var p webfetchParams
	if err := decode(input, &p); err != nil {
		return Result{}, err
	}
	if !strings.HasPrefix(p.URL, "http://") && !strings.HasPrefix(p.URL, "https://") {
		return Result{}, fmt.Errorf("webfetch: url must be http(s)")
	}
	client := w.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("User-Agent", "forge/0.0.1")
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, webfetchLimit))
	if err != nil {
		return Result{}, fmt.Errorf("webfetch: %w", err)
	}
	text := string(body)
	if p.Format != "raw" && strings.Contains(resp.Header.Get("Content-Type"), "html") {
		text = htmlToText(text)
	}
	return Result{Title: p.URL, Output: text,
		Metadata: map[string]any{"status": resp.StatusCode, "url": p.URL}}, nil
}

var (
	tagRe   = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>`)
	htmlTag = regexp.MustCompile(`<[^>]+>`)
	wsRe    = regexp.MustCompile(`\n\s*\n\s*\n+`)
)

// htmlToText is a minimal HTML-to-text reducer (drops script/style + tags).
func htmlToText(html string) string {
	html = tagRe.ReplaceAllString(html, "")
	html = htmlTag.ReplaceAllString(html, "")
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = wsRe.ReplaceAllString(html, "\n\n")
	return strings.TrimSpace(html)
}
