// Package gen holds the server interfaces and request/response models generated
// from the vendored OpenAPI reference (conformance/openapi-reference.json) by
// oapi-codegen. The generated file (opcode42.gen.go) is committed; never edit it
// by hand — regenerate with `make gen`.
//
// Generation is two steps: the reference is 3.1, but oapi-codegen's loader only
// accepts 3.0, so internal/tools/downconvert first emits a derived 3.0 spec
// (a throwaway artifact, gitignored), then oapi-codegen runs against that.
package gen

//go:generate go run github.com/rotemmiz/opcode42/internal/tools/downconvert -in ../../../conformance/openapi-reference.json -out openapi-3.0.derived.json
//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi-3.0.derived.json
