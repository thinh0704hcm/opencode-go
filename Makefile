# opencode-go Makefile
#
# Dev workflow:
#   make build       # compile to ./bin/opencode-go
#   make test-race   # run tests with the race detector
#   make run         # build + serve with the tokenless mock provider
#   make tui         # print the command to attach the real TUI
#
# Mock provider:  OPENCODE_GO_MOCK=1 (no token required)
# Real provider:  OPENCODE_GO_BASE_URL / OPENCODE_GO_API_KEY / OPENCODE_GO_MODEL

PORT ?= 4182
HOST ?= 127.0.0.1
BIN  ?= bin/opencode-go

# Install location for `make deploy`. Defaults to the user-local bin that holds
# the installed wrapper. Override with `make deploy PREFIX=/somewhere/bin`.
PREFIX ?= $(HOME)/.local/bin

.PHONY: help build build-sdk build-wrapper sdk-smoke run run-real test test-race vet fmt fmt-check check tidy clean tui kill deploy

help: ## Print available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build binary to $(BIN)
	go build -o $(BIN) ./cmd/opencode-go

build-sdk: build-wrapper ## Build Go server + SDK wrapper as bin/opencode

build-wrapper: build ## Install wrapper as bin/opencode (expects TS CLI as opencode-ts for TUI path)
	cp scripts/opencode-wrapper bin/opencode
	chmod +x bin/opencode

sdk-smoke: build-sdk ## Run SDK smoke test when /tmp/sdk-extract/package is available
	@if [ ! -d tests/sdk-smoke/node_modules ] || [ ! -f tests/sdk-smoke/package-lock.json ]; then npm install --no-audit --no-fund --prefix tests/sdk-smoke; fi
	OPENCODE_USE_GO_SERVER=1 node --preserve-symlinks tests/sdk-smoke/sdk-smoke.mjs

run: build ## Build + serve with mock provider (foreground)
	OPENCODE_GO_MOCK=1 $(BIN) serve --hostname $(HOST) --port $(PORT)

run-real: build ## Build + serve with real provider (env-configured)
	$(BIN) serve --hostname $(HOST) --port $(PORT)

# Canonical TUI wrapper installed by `make deploy`. Routes the default `opencode`
# TUI to the opencode-go backend via `attach` and auto-starts the backend; passes
# other subcommands through to the real TS binary ($HOME/.opencode/bin/opencode).
# (scripts/opencode-wrapper is the separate SDK serve/models switch used by
# build-sdk/sdk-smoke — do NOT deploy that one.)
WRAPPER ?= scripts/opencode-tui-wrapper

deploy: build ## Install fresh opencode-go + TUI wrapper into $(PREFIX) (default ~/.local/bin)
	@mkdir -p "$(PREFIX)"
	install -m 0755 $(BIN) "$(PREFIX)/opencode-go"
	@echo "installed binary  -> $(PREFIX)/opencode-go"
	@# Back up an existing 'opencode' only when it differs from the wrapper we are
	@# installing; never overwrite anything silently, never invent an opencode-ts.
	@if [ -e "$(PREFIX)/opencode" ] && ! cmp -s "$(WRAPPER)" "$(PREFIX)/opencode"; then \
		bak="$(PREFIX)/opencode.bak-$$(date +%Y%m%d-%H%M%S)"; \
		cp -p "$(PREFIX)/opencode" "$$bak"; \
		echo "backed up differing opencode -> $$bak"; \
	fi
	install -m 0755 $(WRAPPER) "$(PREFIX)/opencode"
	@echo "installed wrapper -> $(PREFIX)/opencode"
	@[ -x "$(HOME)/.opencode/bin/opencode" ] || echo "NOTE: real TS binary $(HOME)/.opencode/bin/opencode not found — set OPENCODE_REAL_BIN"
	@echo "Done. Default 'opencode' attaches the TUI to the opencode-go backend ($(HOME)/opencode-go/bin/opencode-go)."

test: ## Run all tests
	go test ./...

test-race: ## Run all tests with the race detector
	go test -race ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format all Go files in place
	gofmt -w .

fmt-check: ## Fail if any Go files are unformatted
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "unformatted files:"; echo "$$out"; exit 1; fi

check: fmt-check vet test-race ## Run fmt-check + vet + test-race

tidy: ## Tidy go.mod / go.sum
	go mod tidy

clean: ## Remove build output
	rm -rf bin/

tui: ## Print command to attach the real TUI
	@echo "opencode attach http://$(HOST):$(PORT)"

kill: ## Kill any opencode-go serve process on $(PORT)
	pkill -f "opencode-go serve.*--port $(PORT)" || true
