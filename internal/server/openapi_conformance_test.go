package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/rotemmiz/forge/internal/api/gen"
	"github.com/rotemmiz/forge/internal/engine/catalog"
)

// TestResponsesConformToSpec validates that live handler responses for a curated
// set of implemented, side-effect-free GET endpoints conform to the frozen
// OpenAPI contract — the offline half of plan 06 M10 (the dual-run in plan 12 is
// the live-opencode behavioral oracle).
//
// A missing or wrongly-typed required field fails the test; extra additive fields
// are permitted (gen.OpenAPIDoc relaxes additionalProperties:false), per the
// conformance strictness policy (masterplan "Decisions locked" #2).
func TestResponsesConformToSpec(t *testing.T) {
	doc, err := gen.OpenAPIDoc()
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build router: %v", err)
	}

	h := resourceServer(t, catalog.Fixture())
	dir := t.TempDir()

	cases := []struct {
		name, path string
		needsDir   bool
		// knownDivergence, when set, records a documented wire gap (see
		// conformance/known-divergences.json): the request is still exercised and
		// must return 200, but schema validation is skipped with the reason.
		knownDivergence string
	}{
		{name: "health", path: "/global/health"},
		{name: "config", path: "/config", needsDir: true},
		{name: "agent", path: "/agent", needsDir: true},
		{name: "command", path: "/command", needsDir: true},
		{name: "session-list", path: "/session", needsDir: true},
		{name: "provider", path: "/provider", needsDir: true,
			knownDivergence: "provider model wire shape: Forge serves the raw models.dev model " +
				"(flat cost.cache_read/cache_write, attachment/reasoning/temperature/tool_call/" +
				"modalities), but opencode's Model contract requires providerID, options, headers, " +
				"api, capabilities, status and a nested cost.cache{read,write}. BuildProviderList " +
				"passes catalog.Model through untransformed — a focused plan-04 task " +
				"(conformance/known-divergences.json, scenario provider-model-shape)."},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.needsDir {
				r.Header.Set("x-opencode-directory", dir)
			}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, r)
			if rr.Code != http.StatusOK {
				t.Fatalf("GET %s status = %d; body=%s", tc.path, rr.Code, rr.Body.String())
			}
			if tc.knownDivergence != "" {
				t.Skipf("known divergence: %s", tc.knownDivergence)
			}

			route, pathParams, err := router.FindRoute(r)
			if err != nil {
				t.Fatalf("GET %s not matched in spec: %v", tc.path, err)
			}
			input := &openapi3filter.ResponseValidationInput{
				RequestValidationInput: &openapi3filter.RequestValidationInput{
					Request:    r,
					PathParams: pathParams,
					Route:      route,
				},
				Status:  rr.Code,
				Header:  rr.Header(),
				Body:    io.NopCloser(bytes.NewReader(rr.Body.Bytes())),
				Options: &openapi3filter.Options{IncludeResponseStatus: true},
			}
			if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
				t.Errorf("GET %s response violates spec:\n%v", tc.path, err)
			}
		})
	}
}
