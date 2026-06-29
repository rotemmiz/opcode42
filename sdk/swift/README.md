# Opcode42 Swift SDK (future iOS client)

Typed Swift client for the Opcode42 / opencode wire contract, generated from the
frozen contract `conformance/openapi-reference.json` with
[openapi-generator](https://openapi-generator.tech) (`swift6`, async/await),
pinned for deterministic output. Deferred to plan 07's iOS scope (plan 06 M9 /
"Swift SDK (Future / Phase D+)").

## Status: compiles, compile-gated

The generated sources are committed (`gen/`) and kept pinned to the spec
(`make gen-sdks`, drift-checked + compiled by `scripts/check-sdk-fresh.sh` and the
`sdk-fresh` CI job).

The `swift5` generator mis-rendered one array-of-array schema — `QuestionAnswer`
(itself `[String]`) used as a list element came out as an untyped `[Array]`,
breaking two models (`QuestionReplied`, `QuestionReplyRequest`). The fix has two
parts, both deterministic across architectures:

1. **Normalizer (`scripts/gen-sdks.sh` step b2):** at array-item use sites whose
   `items` is a `$ref` to a *bare-array* component schema, the `$ref` is inlined
   to the resolved array schema. This leaves `QuestionAnswer` unreferenced; the
   swift6/kotlin generator skips unreferenced bare-array schemas, so it never reaches
   the generated output (the orphan-collection pass only removes `EventTui*` schemas by
   name — the same normalization discipline the Kotlin fix uses to avoid amd64/arm64
   casing diffs). The wire shape (array of arrays of strings) is unchanged.
2. **`swift6` generator:** with (1) applied, swift6 renders the `answers` field as
   the correct `[[String]]`. (`swift5` still emitted `[Array]` even after the
   inlining — a swift5-specific nested-array defect.) swift6 also emits a
   self-contained SwiftPM `Sources/` layout with **no external `AnyCodable`
   dependency**, so `swift build` compiles offline.

The hand-written SSE / WebSocket-PTY layers (codegen cannot model persistent
streams) are deferred to the iOS client work; see the Kotlin and Go SDKs for the
reference design.

## Layout

- `gen/` — generated models + APIs (`Sources/Opcode42Client/{Models,APIs,Infrastructure}/…`)
  and the SwiftPM manifest (`Package.swift`). **Never edit by hand** — regenerate.

## Building / regenerating

```sh
make gen-sdks                  # regenerates sdk/kotlin/gen and sdk/swift/gen
cd sdk/swift/gen && swift build # compile the generated SDK (needs a Swift toolchain)
make check-sdk-fresh           # drift-check + Swift compile gate (CI parity)
```
