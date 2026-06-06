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

.PHONY: help build run run-real test test-race vet fmt fmt-check check tidy clean tui kill

help: ## Print available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build binary to $(BIN)
	go build -o $(BIN) ./cmd/opencode-go

run: build ## Build + serve with mock provider (foreground)
	OPENCODE_GO_MOCK=1 $(BIN) serve --hostname $(HOST) --port $(PORT)

run-real: build ## Build + serve with real provider (env-configured)
	$(BIN) serve --hostname $(HOST) --port $(PORT)

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
