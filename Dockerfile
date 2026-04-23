# syntax=docker/dockerfile:1
#
# Render.com: sets PORT, RENDER, and DATABASE_URL when a Postgres is linked.
# The server reads PORT from the environment; do not hardcode a listen port at runtime.
# Migrations: set RUN_MIGRATIONS_ON_STARTUP=true in the Render dashboard or render.yaml
# (first deploys need a schema; golang-migrate Up is idempotent).
# WEB_ROOT is set below so GET / serves the Vite app (index.html + /assets/*).

# --- Vite (React) frontend ---
FROM node:20-alpine AS webapp
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

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
COPY --from=webapp /web/dist /app/web

ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENV MIGRATIONS_PATH=/migrations
ENV WEB_ROOT=/app/web
# Default for local `docker run`; Render overrides PORT.
ENV PORT=8080
EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/server"]
