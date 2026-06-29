package spec

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Emit builds the OpenAPI document Opcode42 self-emits from its own route table
// (plan 06 Phase 2 / M10). Unlike Reference() — which serves the frozen contract
// verbatim — the emitted doc's `paths` are derived from the operations the daemon
// actually registered, so removing or re-pathing a handler changes the served
// spec and the drift gate (CompareOps / scripts/check-spec-drift.sh) catches it.
//
// The doc reuses the frozen reference's top-level fields (openapi, info,
// components, ...) so request/response schemas stay byte-identical to the
// contract; only `paths` is rebuilt. For each registered operation:
//   - matched against the reference: the reference operation object is copied
//     verbatim (full request/response contract preserved);
//   - not in the reference (a Opcode42 addition): a minimal operation stub is
//     emitted, tagged `x-opcode42-addition: true` so the diff gate can classify it.
//
// Output is deterministic: Go marshals map keys in sorted order, and operations
// within a path are ordered by method, so the bytes are stable across runs.
func Emit(registered []Operation) ([]byte, error) {
	// Decode the reference preserving raw path-item / operation bodies so matched
	// operations are copied with full fidelity.
	var refDoc map[string]json.RawMessage
	if err := json.Unmarshal(reference, &refDoc); err != nil {
		return nil, fmt.Errorf("parse embedded openapi spec: %w", err)
	}

	var refPaths map[string]map[string]json.RawMessage
	if raw, ok := refDoc["paths"]; ok {
		if err := json.Unmarshal(raw, &refPaths); err != nil {
			return nil, fmt.Errorf("parse reference paths: %w", err)
		}
	}

	// Build the emitted paths object keyed by path, then by method/field.
	emitted := map[string]map[string]json.RawMessage{}
	for _, op := range registered {
		method := lower(op.Method)
		item, ok := emitted[op.Path]
		if !ok {
			item = map[string]json.RawMessage{}
			emitted[op.Path] = item
			// Preserve path-level, non-operation fields (parameters, summary,
			// description, servers) from the reference when the path exists there.
			if refItem, ok := refPaths[op.Path]; ok {
				for field, val := range refItem {
					if !httpMethods[upper(field)] {
						item[field] = val
					}
				}
			}
		}
		if refItem, ok := refPaths[op.Path]; ok {
			if refOp, ok := refItem[method]; ok {
				item[method] = refOp
				continue
			}
		}
		// Opcode42 addition: no matching reference operation. Emit a minimal stub
		// tagged so the diff gate classifies it as additive rather than a contract
		// match.
		stub, err := json.Marshal(map[string]any{
			"x-opcode42-addition": true,
			"operationId":         additionOperationID(op),
			"responses": map[string]any{
				"200": map[string]any{"description": "Opcode42 additive endpoint (not in the frozen contract)"},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("marshal addition stub for %s %s: %w", op.Method, op.Path, err)
		}
		item[method] = stub
	}

	pathsRaw, err := json.Marshal(emitted)
	if err != nil {
		return nil, fmt.Errorf("marshal emitted paths: %w", err)
	}
	refDoc["paths"] = pathsRaw

	out, err := json.MarshalIndent(refDoc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal emitted spec: %w", err)
	}
	return out, nil
}

// EmittedOperations parses an emitted (or reference) spec document and returns
// its operation set with the per-operation set of declared response status codes.
// The drift gate uses this to compare Opcode42's self-emitted spec against the
// frozen reference.
func EmittedOperations(doc []byte) (map[Operation][]string, error) {
	// Path-item values are decoded as raw messages first because a path item may
	// carry non-operation fields (e.g. a `parameters` array or a `summary` string)
	// that are not objects — decoding those directly into an operation struct would
	// fail. We only decode the method entries.
	var parsed struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		return nil, fmt.Errorf("parse emitted spec: %w", err)
	}
	out := map[Operation][]string{}
	for path, item := range parsed.Paths {
		for method, raw := range item {
			m := upper(method)
			if !httpMethods[m] {
				continue // skip parameters, summary, servers, $ref, ...
			}
			var op struct {
				Responses map[string]json.RawMessage `json:"responses"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("parse operation %s %s: %w", m, path, err)
			}
			codes := make([]string, 0, len(op.Responses))
			for code := range op.Responses {
				codes = append(codes, code)
			}
			sort.Strings(codes)
			out[Operation{Method: m, Path: path}] = codes
		}
	}
	return out, nil
}

func additionOperationID(op Operation) string {
	return "opcode42Addition_" + op.Method + "_" + op.Path
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
