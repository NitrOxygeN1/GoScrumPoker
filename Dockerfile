# syntax=docker/dockerfile:1
#
# Render.com: sets PORT, RENDER, and DATABASE_URL when a Postgres is linked.
# The server reads PORT from the environment; do not hardcode a listen port at runtime.
# Migrations: set RUN_MIGRATIONS_ON_STARTUP=true in the Render dashboard or render.yaml
# (first deploys need a schema; golang-migrate Up is idempotent).

# --- build ---
FROM golang:1.22-alpine AS build

RUN apk add --no-cache ca-certificates git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
	go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- runtime (static binary + CA bundle for outbound TLS, e.g. OAuth) ---
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/server /server
COPY --from=build /src/migrations /migrations

ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENV MIGRATIONS_PATH=/migrations
# Default for local `docker run`; Render overrides PORT.
ENV PORT=8080
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/server"]
