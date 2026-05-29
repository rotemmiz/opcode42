package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	h, err := New(Options{Version: "test-1.2.3"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/global/health")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var body struct {
		Healthy bool   `json:"healthy"`
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.Healthy || body.Version != "test-1.2.3" {
		t.Errorf("unexpected health body: %+v", body)
	}
}

func TestDocServesOpenAPISpec(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/doc")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: want 200, got %d", resp.StatusCode)
	}
	var doc struct {
		OpenAPI string         `json:"openapi"`
		Paths   map[string]any `json:"paths"`
		Info    map[string]any `json:"info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatalf("decode /doc: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Errorf("openapi version: want 3.1.0 (wire-compat), got %q", doc.OpenAPI)
	}
	if len(doc.Paths) != 113 {
		t.Errorf("paths: want 113, got %d", len(doc.Paths))
	}
}

func TestUnimplementedOperationReturns501(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t))
	defer srv.Close()

	// /session GET is in the spec but not implemented in Phase A.
	resp, err := http.Get(srv.URL + "/session")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status: want 501, got %d", resp.StatusCode)
	}
	var body struct {
		Tag       string `json:"_tag"`
		Operation string `json:"operation"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Tag != "NotImplemented" {
		t.Errorf("_tag: want NotImplemented, got %q", body.Tag)
	}
}

func TestUnknownPathReturns404(t *testing.T) {
	srv := httptest.NewServer(newTestServer(t))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/definitely/not/a/route")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: want 404 for unknown path, got %d", resp.StatusCode)
	}
}
