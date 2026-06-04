# Forge Swift SDK (scaffold — future iOS client)

Typed Swift client for the Forge / opencode wire contract, generated from the
frozen contract `conformance/openapi-reference.json` with
[openapi-generator](https://openapi-generator.tech) (`swift5`, async/await),
pinned for deterministic output. Deferred to plan 07's iOS scope (plan 06 M9 /
"Swift SDK (Future / Phase D+)").

## Status: scaffold, not yet compile-gated

The generated sources are committed (`gen/`) and kept pinned to the spec
(`make gen-sdks`, drift-checked by `scripts/check-sdk-fresh.sh`), but the Swift
package is **not** built in CI yet. The `swift5` generator currently mis-renders
one array-of-array schema — `QuestionAnswer` (itself `[String]`) used as a list
element comes out as an untyped `[Array]` — which breaks two models
(`QuestionReplied`, `QuestionReplyRequest`). The fix (a normalizer rule, an
`--type-mappings` override, or the newer `swift6` generator) is a same-track SDK
followup.

The hand-written SSE / WebSocket-PTY layers (codegen cannot model persistent
streams) are also deferred to the iOS client work; see the Kotlin and Go SDKs for
the reference design.

## Layout

- `gen/` — generated models + APIs (`ForgeClient/Classes/OpenAPIs/…`) and the
  SwiftPM manifest (`Package.swift`). **Never edit by hand** — regenerate.

## Regenerating

```sh
make gen-sdks          # regenerates sdk/kotlin/gen and sdk/swift/gen
```
