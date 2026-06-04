#!/usr/bin/env bash
# Generate the Kotlin + Swift client SDKs from the frozen wire contract
# (conformance/openapi-reference.json) using openapi-generator-cli, pinned to a
# fixed version for deterministic, reviewable output (plan 06 M5 / M9).
#
# Pipeline (mirrors the Go SDK's, which downconverts before oapi-codegen):
#   1. downconvert the 3.1 reference -> a derived 3.0 spec (the same converter the
#      Go SDK uses, internal/tools/downconvert). openapi-generator handles 3.0 far
#      better than 3.1 (3.1's `anyOf:[…,{type:null}]` nullability and free-form
#      `anyOf:[{}]` shapes otherwise produce non-compiling output).
#   2. normalize a few residual free-form/union shapes the generators still botch
#      (see comments on the python step) — representation-only, wire shape kept.
#   3. run the Kotlin (and best-effort Swift) generators.
#
# The Go SDK is generated separately by `make gen` (oapi-codegen via go generate);
# this script covers only the JVM/Swift generators that need a Java toolchain.
#
# Output is committed (sdk/kotlin/gen, sdk/swift/gen); CI re-runs this and fails on
# any diff (scripts/check-sdk-fresh.sh) so the SDKs stay pinned to the spec.
#
# Usage: scripts/gen-sdks.sh                  # generate into the repo (in place)
#        OUT_DIR=/tmp/x scripts/gen-sdks.sh   # generate elsewhere (freshness check)
#
# Requires: java (>= 11) and go (for the downconvert step). The openapi-generator
# JAR is downloaded once and cached under ${OPENAPI_GENERATOR_CACHE:-~/.cache/forge-sdk};
# set OPENAPI_GENERATOR_JAR to a pre-downloaded JAR for offline/CI use.
set -euo pipefail

# --- pinned toolchain -------------------------------------------------------
OPENAPI_GENERATOR_VERSION="${OPENAPI_GENERATOR_VERSION:-7.10.0}"
KOTLIN_PACKAGE="dev.forge.sdk"
SWIFT_PROJECT="ForgeClient"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SPEC="$REPO_ROOT/conformance/openapi-reference.json"
OUT_DIR="${OUT_DIR:-$REPO_ROOT}"
CACHE_DIR="${OPENAPI_GENERATOR_CACHE:-$HOME/.cache/forge-sdk}"
GO="${GO:-go}"

if [[ ! -f "$SPEC" ]]; then
  echo "error: spec not found at $SPEC (run scripts/sync-openapi.sh)" >&2
  exit 2
fi

if ! command -v java >/dev/null 2>&1; then
  echo "error: java not found — the Kotlin/Swift SDK generators need a JVM (>= 11)." >&2
  echo "       Install a JDK, or skip SDK gen (Go SDK still regenerates via 'make gen')." >&2
  exit 3
fi

# --- resolve the generator JAR (cache or download) --------------------------
JAR="${OPENAPI_GENERATOR_JAR:-}"
if [[ -z "$JAR" ]]; then
  JAR="$CACHE_DIR/openapi-generator-cli-$OPENAPI_GENERATOR_VERSION.jar"
  if [[ ! -f "$JAR" ]]; then
    mkdir -p "$CACHE_DIR"
    URL="https://repo1.maven.org/maven2/org/openapitools/openapi-generator-cli/$OPENAPI_GENERATOR_VERSION/openapi-generator-cli-$OPENAPI_GENERATOR_VERSION.jar"
    echo "downloading openapi-generator-cli $OPENAPI_GENERATOR_VERSION ..." >&2
    if ! curl -fsSL -o "$JAR.tmp" "$URL"; then
      echo "error: could not download $URL (set OPENAPI_GENERATOR_JAR to a local copy for offline use)." >&2
      rm -f "$JAR.tmp"
      exit 4
    fi
    mv "$JAR.tmp" "$JAR"
  fi
fi

# --- 1. downconvert 3.1 -> derived 3.0 --------------------------------------
DERIVED_SPEC="$(mktemp -t forge-sdk-3.0.XXXXXX.json)"
NORM_SPEC="$(mktemp -t forge-sdk-norm.XXXXXX.json)"
trap 'rm -f "$DERIVED_SPEC" "$NORM_SPEC"' EXIT
echo "== downconvert 3.1 -> 3.0 ==" >&2
# No -client flag: that disambiguator is specific to oapi-codegen's response
# wrappers and would rename component schemas the openapi-generator output keeps.
"$GO" run github.com/rotemmiz/forge/internal/tools/downconvert \
  -in "$SPEC" -out "$DERIVED_SPEC" >/dev/null

# --- 2. normalize residual shapes -------------------------------------------
# Two representation-only transforms (wire shape preserved — request/response
# bytes are unchanged), each working around an openapi-generator defect:
#   (a) free-form object map values (`additionalProperties: {type: object}`) ->
#       `additionalProperties: true`. Combined with the `object/AnyType ->
#       JsonElement` type-mapping below, the Kotlin generator then emits a
#       kotlinx-serializable `Map<String, JsonElement>` instead of an
#       unserializable `Map<String, Any>`.
#   (b) a requestBody whose ROOT schema is an undiscriminated `anyOf` of object
#       `$ref`s (only /tui/publish: a union of 4 EventTui* schemas) -> free-form
#       object. The Kotlin generator otherwise merges this into one inline model
#       that references the members with mangled casing (`Eventtuitoastshow` vs
#       the imported `EventTuiToastShow`) and fails to compile. Collapsing this
#       one body to free-form matches what the Swift generator produces and still
#       lets the client POST any union member verbatim.
python3 - "$DERIVED_SPEC" "$NORM_SPEC" <<'PY'
import json, sys
src, dst = sys.argv[1], sys.argv[2]
spec = json.load(open(src))

def is_ref(s):
    return isinstance(s, dict) and set(s.keys()) == {"$ref"}

# (a) free-form map values -> additionalProperties: true
def norm_freeform_maps(node):
    if isinstance(node, dict):
        ap = node.get("additionalProperties")
        if (isinstance(ap, dict) and ap.get("type") == "object"
                and "properties" not in ap and "additionalProperties" not in ap
                and "$ref" not in ap):
            node["additionalProperties"] = True
        for v in list(node.values()):
            norm_freeform_maps(v)
    elif isinstance(node, list):
        for v in node:
            norm_freeform_maps(v)

norm_freeform_maps(spec)

# (b) requestBody root anyOf-of-object-refs -> free-form object
for _path, item in spec.get("paths", {}).items():
    if not isinstance(item, dict):
        continue
    for _method, op in item.items():
        if not isinstance(op, dict):
            continue
        for _ct, media in (op.get("requestBody", {}) or {}).get("content", {}).items():
            schema = media.get("schema")
            if (isinstance(schema, dict) and isinstance(schema.get("anyOf"), list)
                    and len(schema["anyOf"]) > 1
                    and all(is_ref(s) for s in schema["anyOf"])
                    and "discriminator" not in schema):
                schema.clear()
                schema["type"] = "object"
                schema["additionalProperties"] = True

# (b2) inline bare-array component schemas at array-item use sites. The swift5
#      generator does not emit a named model for a component schema that is itself a
#      bare array (e.g. `QuestionAnswer = {type: array, items: {type: string}}`); when
#      such a schema is used as the `items` of another array (`answers: [[String]]`)
#      it falls back to the bare `Array` type and emits the non-compiling `[Array]`.
#      Replacing the `$ref` with the resolved array schema makes the generator render
#      `[[String]]` directly. Representation-only: the wire shape (array of arrays of
#      strings) is preserved. After this, QuestionAnswer is unreferenced; the swift6/
#      kotlin generator skips unreferenced bare-array schemas, so it never reaches the
#      generated output. (The orphan-collection pass (c) below is name-restricted to
#      EventTui* schemas and does NOT remove QuestionAnswer.)
def is_bare_array_schema(s):
    return (isinstance(s, dict) and s.get("type") == "array" and "items" in s
            and "properties" not in s and "allOf" not in s
            and "anyOf" not in s and "oneOf" not in s)

def resolve_ref(ref):
    if not isinstance(ref, str) or not ref.startswith("#/components/schemas/"):
        return None
    return spec.get("components", {}).get("schemas", {}).get(ref.rsplit("/", 1)[1])

def inline_bare_array_item_refs(node):
    if isinstance(node, dict):
        items = node.get("items")
        if node.get("type") == "array" and is_ref(items):
            target = resolve_ref(items["$ref"])
            if is_bare_array_schema(target):
                node["items"] = json.loads(json.dumps(target))
        for v in node.values():
            inline_bare_array_item_refs(v)
    elif isinstance(node, list):
        for v in node:
            inline_bare_array_item_refs(v)

# Walk paths and schema definitions; inlining copies the resolved array schema so
# the source component is left intact for any other (non-bare-array) use sites.
inline_bare_array_item_refs(spec.get("paths", {}))
inline_bare_array_item_refs(spec.get("components", {}).get("schemas", {}))

# (c) drop component schemas left UNREFERENCED after (b). Collapsing /tui/publish
#     orphans the four EventTui{PromptAppend,CommandExecute,ToastShow,SessionSelect}
#     schemas (the Event union references the downconverted *1 variants, not these).
#     The Kotlin generator still emits the orphans, and their names collide with the
#     *1 variants, which triggers an ARCH-DEPENDENT casing bug (`EventTuiToastShow`
#     on amd64 vs `Eventtuitoastshow` on arm64) — non-reproducible committed output.
#     Removing the dead schemas removes the ambiguity entirely. Generic: any
#     component schema with zero remaining `$ref`s after (a)/(b) is dropped.
def collect_refs(node, acc):
    if isinstance(node, dict):
        r = node.get("$ref")
        if isinstance(r, str) and r.startswith("#/components/schemas/"):
            acc.add(r.rsplit("/", 1)[1])
        for v in node.values():
            collect_refs(v, acc)
    elif isinstance(node, list):
        for v in node:
            collect_refs(v, acc)

schemas = spec.get("components", {}).get("schemas", {})
# Iterate to a fixed point: dropping a schema can orphan others it referenced.
while True:
    referenced = set()
    # refs from paths/operations
    collect_refs(spec.get("paths", {}), referenced)
    # refs between schemas (a schema is "live" only if reachable from an operation,
    # but to stay conservative we keep any schema referenced by another KEPT schema)
    collect_refs(schemas, referenced)
    dead = [n for n in schemas
            if n not in referenced and n.startswith("EventTui")]
    if not dead:
        break
    for n in dead:
        del schemas[n]

json.dump(spec, open(dst, "w"), indent=2, sort_keys=True)
PY

# --- 3. generate ------------------------------------------------------------
gen() {
  local generator="$1" out="$2"; shift 2
  echo "== generating $generator -> $out ==" >&2
  # --skip-validate-spec: the frozen opencode contract has a duplicate `pty` tag
  # (a benign upstream quirk) carried through the downconvert.
  # --global-property *Docs/*Tests=false: no per-endpoint docs/test stubs.
  java -jar "$JAR" generate \
    -i "$NORM_SPEC" \
    -g "$generator" \
    -o "$out" \
    --skip-validate-spec \
    --global-property=apiDocs=false,modelDocs=false,apiTests=false,modelTests=false \
    "$@" >/dev/null
}

# --- Kotlin (Android primary client) — fully built, compiles clean ----------
KOTLIN_OUT="$OUT_DIR/sdk/kotlin/gen"
rm -rf "$KOTLIN_OUT"
mkdir -p "$KOTLIN_OUT"
cp "$REPO_ROOT/sdk/kotlin/.openapi-generator-ignore" "$KOTLIN_OUT/.openapi-generator-ignore"
# model-name-mappings File=ForgeFile: the `File` schema otherwise collides with
# the Kotlin generator's reserved `java.io.File` mapping (corrupts the class name).
# (The EventTui* casing/arch issue is handled upstream by normalizer step (c),
# which drops those now-unreferenced schemas entirely.)
gen kotlin "$KOTLIN_OUT" \
  --library jvm-okhttp4 \
  --model-name-mappings 'File=ForgeFile' \
  --type-mappings 'AnyType=JsonElement,object=JsonElement' \
  --import-mappings 'JsonElement=kotlinx.serialization.json.JsonElement' \
  --additional-properties="packageName=$KOTLIN_PACKAGE,dateLibrary=java8,serializationLibrary=kotlinx_serialization,useCoroutines=true"

# --- Swift (future iOS client, plan 07 stretch) — compiles, compile-gated ----
# Uses the `swift6` generator: combined with the bare-array-ref inlining in
# normalizer step (b2), it renders the array-of-array `answers` field as the
# correct `[[String]]`. The older `swift5` generator emits the non-compiling
# `[Array]` even after (b2) (a swift5-specific nested-array defect). swift6 emits
# a SwiftPM `Sources/` layout that `swift build` compiles clean; the freshness
# gate (scripts/check-sdk-fresh.sh) compile-gates it when a Swift toolchain is
# present and always asserts the committed tree matches this output.
SWIFT_OUT="$OUT_DIR/sdk/swift/gen"
rm -rf "$SWIFT_OUT"
mkdir -p "$SWIFT_OUT"
cp "$REPO_ROOT/sdk/swift/.openapi-generator-ignore" "$SWIFT_OUT/.openapi-generator-ignore"
gen swift6 "$SWIFT_OUT" \
  --additional-properties="projectName=$SWIFT_PROJECT,responseAs=AsyncAwait"

# --- 4. Linux-portability patch for the swift `urlsession` template ----------
# The swift6 generator's URLSession-based infrastructure is written Apple-first
# and does NOT compile on Linux (the CI compile gate runs `swift build` on
# ubuntu). Three template defects, all in URLSessionImplementations.swift:
#   (i)  `#if !os(macOS) import MobileCoreServices` — MobileCoreServices is an
#        Apple-only framework, so on Linux (where `!os(macOS)` is true) the import
#        fails with "no such module". Replace the OS guard with a capability check
#        (`#if canImport(MobileCoreServices)`), which is false on Linux.
#   (ii) the file uses `URLRequest`/`URLResponse`/`URLSession*` but never imports
#        `FoundationNetworking`, where those types live on Linux (Foundation only
#        re-exports them on Apple platforms). Every OTHER infra file the generator
#        emits already has the `#if canImport(FoundationNetworking)` block; this
#        one is missing it. Add the same block.
#   (iii) the pre-macOS-11 `else` fallback in `mimeType(for:)` calls the legacy
#        MobileCoreServices C API (`UTTypeCreatePreferredIdentifierForTag`,
#        `kUTTagClass*`), which is unavailable on Linux. Guard that body with
#        `#if canImport(MobileCoreServices)` so Linux falls through to the existing
#        `application/octet-stream` default.
# All three are deterministic string substitutions with an exact-match assertion,
# so regen stays byte-identical (the freshness diff gate holds). Wire/behaviour on
# Apple platforms is unchanged: `canImport(MobileCoreServices)` is true there, and
# `FoundationNetworking` is absent there so its `canImport` block is skipped.
echo "== patching swift urlsession template for Linux portability ==" >&2
python3 - "$SWIFT_OUT/Sources/$SWIFT_PROJECT/Infrastructure/URLSessionImplementations.swift" <<'PY'
import sys
f = sys.argv[1]
s = open(f).read()

def sub(old, new, s):
    n = s.count(old)
    if n != 1:
        sys.stderr.write(
            "error: swift Linux patch: expected exactly 1 match for a "
            "URLSessionImplementations.swift fragment, found %d — the swift6 "
            "generator output drifted; re-check scripts/gen-sdks.sh step 4.\n" % n)
        sys.exit(1)
    return s.replace(old, new)

# (i) + (ii): replace the Apple-only OS guard and add the FoundationNetworking
# import block in one substitution anchored on `import Foundation`.
s = sub(
    "import Foundation\n#if !os(macOS)\nimport MobileCoreServices\n#endif",
    "import Foundation\n"
    "#if canImport(FoundationNetworking)\nimport FoundationNetworking\n#endif\n"
    "#if canImport(MobileCoreServices)\nimport MobileCoreServices\n#endif",
    s)

# (iii): guard the legacy MobileCoreServices C-API fallback.
s = sub(
    "        } else {\n"
    "            if let uti = UTTypeCreatePreferredIdentifierForTag(kUTTagClassFilenameExtension, pathExtension as NSString, nil)?.takeRetainedValue(),\n"
    "                    let mimetype = UTTypeCopyPreferredTagWithClass(uti, kUTTagClassMIMEType)?.takeRetainedValue() {\n"
    "                return mimetype as String\n"
    "            }\n"
    "            return \"application/octet-stream\"\n"
    "        }",
    "        } else {\n"
    "            #if canImport(MobileCoreServices)\n"
    "            if let uti = UTTypeCreatePreferredIdentifierForTag(kUTTagClassFilenameExtension, pathExtension as NSString, nil)?.takeRetainedValue(),\n"
    "                    let mimetype = UTTypeCopyPreferredTagWithClass(uti, kUTTagClassMIMEType)?.takeRetainedValue() {\n"
    "                return mimetype as String\n"
    "            }\n"
    "            #endif\n"
    "            return \"application/octet-stream\"\n"
    "        }",
    s)

open(f, "w").write(s)
PY

echo "SDKs generated under $OUT_DIR/sdk/{kotlin,swift}/gen" >&2
