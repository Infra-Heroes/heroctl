SERVER_URL  ?= https://api.hero.example.com
AUTH_DOMAIN ?= auth.example.com
CLIENT_ID   ?=

.PHONY: build
build:
	go build -ldflags "\
	  -X github.com/Infra-Heroes/heroctl/internal/build.ServerURL=$(SERVER_URL) \
	  -X github.com/Infra-Heroes/heroctl/internal/build.AuthDomain=$(AUTH_DOMAIN) \
	  -X github.com/Infra-Heroes/heroctl/internal/build.ClientID=$(CLIENT_ID)" \
	  -o heroctl ./cmd/heroctl
