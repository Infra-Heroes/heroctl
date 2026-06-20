# Build stage
FROM golang:alpine AS builder

WORKDIR /app
RUN apk add --no-cache make git

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG SERVER_URL
ARG AUTH_DOMAIN
ARG CLIENT_ID
ENV HERO_SERVER_URL=$SERVER_URL
ENV HERO_AUTH_DOMAIN=$AUTH_DOMAIN
ENV HERO_CLIENT_ID=$CLIENT_ID
RUN make build

# Final stage
FROM docker:cli

# Install git, since heroctl deploy uses git rev-parse for image tags
RUN apk add --no-cache git

COPY --from=builder /app/heroctl /usr/local/bin/heroctl

ENTRYPOINT ["/usr/local/bin/heroctl"]
