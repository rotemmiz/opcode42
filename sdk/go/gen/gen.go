// Package gen holds the generated Go REST client (request functions +
// request/response models) for the Opcode42 / opencode wire contract, produced by
// oapi-codegen. The generated file (client.gen.go) is committed; never edit it
// by hand — regenerate with `make gen`.
//
// Generation mirrors the server interface (internal/api/gen): the 3.1 reference
// is downconverted to a derived 3.0 spec (a gitignored throwaway), then
// oapi-codegen runs against it with client+models. The -client flag additionally
// disambiguates schema names that collide with client "<OperationId>Response"
// wrappers (a collision the server generator never hits).
//
// The hand-written Opcode42Client wrapper (auth/directory injection, SSE, WS-PTY)
// lives one level up in package opcode42client.
package gen

//go:generate go run github.com/rotemmiz/opcode42/internal/tools/downconvert -client -in ../../../conformance/openapi-reference.json -out openapi-3.0.derived.json
//go:generate go tool oapi-codegen -config oapi-codegen.yaml openapi-3.0.derived.json
