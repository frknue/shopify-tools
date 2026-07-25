BINARY      := shopify-tools
PKG         := github.com/frknue/shopify-tools
BUILD_DIR   := bin
MAIN        := ./cmd/shopify-tools

VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse -q --verify HEAD 2>/dev/null || echo unknown)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(PKG)/internal/buildinfo.version=$(VERSION) \
	-X $(PKG)/internal/buildinfo.commit=$(COMMIT) \
	-X $(PKG)/internal/buildinfo.date=$(DATE)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary into ./bin
	@mkdir -p $(BUILD_DIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BUILD_DIR)/$(BINARY) $(MAIN)

.PHONY: install
install: ## Install the binary into GOBIN
	go install -trimpath -ldflags '$(LDFLAGS)' $(MAIN)

.PHONY: run
run: ## Run the CLI: make run ARGS="auth list"
	go run $(MAIN) $(ARGS)

.PHONY: test
test: ## Run the test suite
	go test -race -shuffle=on ./...

.PHONY: cover
cover: ## Run tests and open the coverage report
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out

.PHONY: lint
lint: ## Run golangci-lint (go tool golangci-lint)
	go tool golangci-lint run ./...

.PHONY: fmt
fmt: ## Format the code
	gofmt -s -w .
	go mod tidy

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: check
check: fmt vet lint test ## Everything CI runs

.PHONY: completions
completions: build ## Generate shell completions into ./completions
	@mkdir -p completions
	@for sh in bash zsh fish; do $(BUILD_DIR)/$(BINARY) completion $$sh > completions/$(BINARY).$$sh; done
	@echo "wrote completions/"

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR) completions coverage.out
