BINARY     := heroctl
GO         := go
GOFLAGS    ?=
SERVER_URL  ?= $(or $(HERO_SERVER_URL),https://api.hero.example.com)
AUTH_DOMAIN ?= $(or $(HERO_AUTH_DOMAIN),auth.example.com)
CLIENT_ID   ?= $(HERO_CLIENT_ID)

GOLANGCI_LINT_VERSION := latest

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_/-]+:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*##"}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the heroctl binary
	$(GO) build $(GOFLAGS) -ldflags "\
	  -X github.com/Infra-Heroes/heroctl/internal/build.ServerURL=$(SERVER_URL) \
	  -X github.com/Infra-Heroes/heroctl/internal/build.AuthDomain=$(AUTH_DOMAIN) \
	  -X github.com/Infra-Heroes/heroctl/internal/build.ClientID=$(CLIENT_ID)" \
	  -o $(BINARY) ./cmd/heroctl

.PHONY: test
test: ## Run tests with race detector and coverage
	$(GO) test -race -cover -coverprofile=coverage.txt ./...
	$(GO) tool cover -func=coverage.txt

.PHONY: lint
lint: ## Run golangci-lint (installs if missing)
	@which golangci-lint > /dev/null 2>&1 || \
		{ echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION)..."; \
		  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $$(go env GOPATH)/bin $(GOLANGCI_LINT_VERSION); }
	golangci-lint run ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: security
security: ## Run govulncheck and gosec
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...
	$(GO) install github.com/securego/gosec/v2/cmd/gosec@latest
	gosec ./...

.PHONY: check
check: vet lint test ## Run all checks (vet, lint, test)

.PHONY: fmt
fmt: ## Format the code
	$(GO) fmt ./...

.PHONY: tidy
tidy: ## Tidy go modules
	$(GO) mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.txt
