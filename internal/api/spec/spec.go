// Package spec embeds the frozen OpenAPI wire contract and exposes it to the
// daemon. openapi.json is a derived copy of the canonical reference at
// conformance/openapi-reference.json (written by scripts/sync-openapi.sh); the
// guard test in this package asserts the two stay byte-identical. The daemon
// serves these bytes verbatim at GET /doc to match opencode (whose live spec is
// served at /doc, not /openapi.json — see internal/server).
package spec

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed openapi.json
var reference []byte

// Reference returns the raw OpenAPI 3.1 document bytes, ready to serve at /doc.
func Reference() []byte { return reference }

// Operation is a single (METHOD, PATH-template) pair from the spec. Paths use
// OpenAPI's {param} syntax, which is identical to chi's, so they bind directly.
type Operation struct {
	Method string // upper-case HTTP method
	Path   string // e.g. "/session/{id}"
}

var httpMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true, "PATCH": true,
}

// Operations parses the embedded spec and returns every documented operation,
// sorted for deterministic route registration.
func Operations() ([]Operation, error) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(reference, &doc); err != nil {
		return nil, fmt.Errorf("parse embedded openapi spec: %w", err)
	}
	var ops []Operation
	for path, item := range doc.Paths {
		for method := range item {
			m := upper(method)
			if !httpMethods[m] {
				continue // skip parameters, $ref, summary, etc.
			}
			ops = append(ops, Operation{Method: m, Path: path})
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Path != ops[j].Path {
			return ops[i].Path < ops[j].Path
		}
		return ops[i].Method < ops[j].Method
	})
	return ops, nil
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}
