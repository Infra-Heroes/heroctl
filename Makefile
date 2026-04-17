BINARY     := heroctl
GO         := go
GOFLAGS    ?=
SERVER_URL  ?= https://api.hero.example.com
AUTH_DOMAIN ?= auth.example.com
CLIENT_ID   ?=

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
test: ## Run tests
	$(GO) test -race ./...

.PHONY: fmt
fmt: ## Format the code
	$(GO) fmt ./...

.PHONY: tidy
tidy: ## Tidy go modules
	$(GO) mod tidy

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
