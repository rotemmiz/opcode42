GO      ?= go
BIN_DIR := bin
DAEMON  := $(BIN_DIR)/forged
TARGET  ?= http://localhost:4096

.PHONY: all build test lint gen tidy conformance record selfdiff release-snapshot clean help

all: build

build: ## Build the forged daemon into bin/forged
	$(GO) build -o $(DAEMON) ./cmd/forged

release-snapshot: ## Dry-run the release build (binaries + archives, no publish) — plan 13
	goreleaser release --snapshot --clean --skip=docker

test: ## Run unit tests
	$(GO) test ./...

lint: ## Run golangci-lint
	golangci-lint run

gen: ## Regenerate code from the OpenAPI reference (oapi-codegen) — task S3
	$(GO) generate ./...

tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

conformance: ## Run the conformance suite against TARGET=<url> — task C3+
	$(GO) test ./conformance/... -target=$(TARGET)

record: ## Record opencode truth cassettes (needs a running opencode) — task C2
	$(GO) run ./conformance/cmd/record -url $(TARGET) -out conformance/cassettes/sse-catalog.json

selfdiff: ## Run the opencode-vs-opencode conformance self-diff gate (task C7)
	bash scripts/run-conformance.sh self

clean: ## Remove build artifacts
	rm -rf $(BIN_DIR)

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
