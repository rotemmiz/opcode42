// This file is hand-written (not generated) and lives in package gen because it
// embeds the same derived OpenAPI 3.0 document that oapi-codegen consumes.
//
// It backs plan 06 M10 (handler<->spec conformance): the daemon's live handler
// responses are validated against the frozen wire contract offline, without a
// running opencode. kin-openapi is OpenAPI 3.0 only, so we validate against the
// downconverted spec (openapi-3.0.derived.json) rather than the 3.1 reference.
package gen

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
)

//go:embed openapi-3.0.derived.json
var derivedSpec []byte

var (
	docOnce sync.Once
	docVal  *openapi3.T
	docErr  error
)

// OpenAPIDoc returns the loaded OpenAPI 3.0 document derived from the frozen
// reference. Callers use it (with openapi3filter) to assert that live handler
// responses conform to the wire contract.
//
// Per the conformance strictness policy (masterplan "Decisions locked" #2:
// missing/changed field = fail, extra additive field = warn), explicit
// `additionalProperties: false` constraints are relaxed to "unspecified" so that
// responses carrying extra fields are permitted. Schema-typed additionalProperties
// (record/map values) are left intact.
func OpenAPIDoc() (*openapi3.T, error) {
	docOnce.Do(func() {
		doc, err := openapi3.NewLoader().LoadFromData(derivedSpec)
		if err != nil {
			docErr = fmt.Errorf("load derived openapi 3.0 spec: %w", err)
			return
		}
		seen := map[*openapi3.SchemaRef]bool{}
		for _, ref := range doc.Components.Schemas {
			allowExtraFields(ref, seen)
		}
		for _, item := range doc.Paths.Map() {
			for _, op := range item.Operations() {
				for _, resp := range op.Responses.Map() {
					if resp.Value == nil {
						continue
					}
					for _, media := range resp.Value.Content {
						allowExtraFields(media.Schema, seen)
					}
				}
			}
		}
		docVal = doc
	})
	return docVal, docErr
}

// allowExtraFields walks a schema graph and flips every explicit
// `additionalProperties: false` to unspecified (extras allowed), tracking visited
// refs so resolved $ref cycles terminate.
func allowExtraFields(ref *openapi3.SchemaRef, seen map[*openapi3.SchemaRef]bool) {
	if ref == nil || ref.Value == nil || seen[ref] {
		return
	}
	seen[ref] = true
	s := ref.Value
	if s.AdditionalProperties.Has != nil && !*s.AdditionalProperties.Has {
		s.AdditionalProperties.Has = nil
	}
	for _, p := range s.Properties {
		allowExtraFields(p, seen)
	}
	allowExtraFields(s.Items, seen)
	allowExtraFields(s.Not, seen)
	allowExtraFields(s.AdditionalProperties.Schema, seen)
	for _, sub := range s.AllOf {
		allowExtraFields(sub, seen)
	}
	for _, sub := range s.AnyOf {
		allowExtraFields(sub, seen)
	}
	for _, sub := range s.OneOf {
		allowExtraFields(sub, seen)
	}
}
