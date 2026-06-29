package spec

import (
	"encoding/json"
	"fmt"
)

// ParseKnownAdditions decodes the known-additions registry
// (conformance/known-additions.json) into a lookup set. The registry is a JSON
// array of [METHOD, PATH] pairs naming endpoints Opcode42 intentionally adds beyond
// the frozen contract (e.g. ["GET", "/openapi.json"]). The drift gate downgrades
// these from FAIL to WARN.
func ParseKnownAdditions(data []byte) (map[Operation]bool, error) {
	var pairs [][]string
	if err := json.Unmarshal(data, &pairs); err != nil {
		return nil, fmt.Errorf("parse known-additions: %w", err)
	}
	out := map[Operation]bool{}
	for _, p := range pairs {
		if len(p) != 2 {
			return nil, fmt.Errorf("known-additions entry must be [method, path], got %v", p)
		}
		out[Operation{Method: upper(p[0]), Path: p[1]}] = true
	}
	return out, nil
}
