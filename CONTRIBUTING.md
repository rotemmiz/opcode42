# Contributing to Opcode42

## Prerequisites

- Go 1.22+
- [`golangci-lint`](https://golangci-lint.run/usage/install/) on your `$PATH`

## Building and Running

```sh
make build        # compiles bin/opcoded
./bin/opcoded serve
```

Or without Make:

```sh
go build -o bin/opcoded ./cmd/opcoded
./bin/opcoded serve
```

## Running Tests

```sh
make test                             # unit tests
make conformance TARGET=<url>         # conformance suite against a running daemon
make selfdiff                         # opencode-vs-opencode self-diff gate
```

Or directly:

```sh
go test ./...
scripts/run-conformance.sh self
```

## Git Workflow

1. **Branch from main.**
   ```sh
   git checkout main && git pull
   git checkout -b <feature>
   ```
2. **Build the feature.** Commit as the work warrants. No `Co-Authored-By` lines.
3. **Run the local CI gate** (see below) before pushing. CI minutes are exhausted — do not rely on GitHub Actions.
4. **Push and open a PR.**
   ```sh
   git push -u origin <feature>
   gh pr create
   ```
5. **Iterate:** spin a local review subagent, apply fixes, re-run the gate, repeat until clean.
6. **Merge via GitHub:** `gh pr merge`, then sync main locally.

## Local CI Gate

Run all of the following before pushing. A PR is not ready until every check passes cleanly.

```sh
go build ./...
go vet ./...
gofmt -l .                                  # output must be empty
golangci-lint run
go test ./...
make gen && git diff --exit-code internal/api/gen/
scripts/run-conformance.sh self
```

If the change adds or modifies an endpoint, also run a dual-run diff against a real opencode daemon and record any intentional divergence in the known-divergence registry (`plans/12-test-compatibility.md`).

## Code Style

- Standard Go idioms. Run `gofmt` before committing.
- Comments only when the **why** is non-obvious. Don't restate what the code says.
- No fabricated performance numbers. Any perf claim requires a head-to-head measurement against opencode (see `plans/00-masterplan.md`).
- Wire compatibility is non-negotiable by default. Match opencode's endpoint shapes, SSE `{ id, type, properties }` envelope, PTY framing (`0x00` + UTF-8 JSON `{cursor}`), and auth/routing conventions. Log intentional divergences; don't silently break them.
- Vetted libraries only — additions must be justified in `DEPENDENCIES.md` and cross-checked against `plans/`.
