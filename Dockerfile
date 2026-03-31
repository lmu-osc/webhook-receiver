# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder
WORKDIR /src

# Cache dependencies first
COPY go.mod go.sum ./
RUN go mod download

# Build static Linux binary
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /out/webhook-receiver .

FROM alpine:3.22
WORKDIR /app

# Runtime requirements: TLS certs for git over HTTPS + git for clone/pull
RUN apk add --no-cache ca-certificates git su-exec && update-ca-certificates

# Run as non-root
RUN addgroup -S app && adduser -S -G app app

COPY --from=builder /out/webhook-receiver /app/webhook-receiver
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chown -R app:app /app
RUN chmod +x /app/docker-entrypoint.sh

EXPOSE 8080

ENTRYPOINT ["/app/docker-entrypoint.sh"]
