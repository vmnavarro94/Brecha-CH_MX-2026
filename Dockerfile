# Multi-stage build: bundle the frontend, compile the Go binary, ship a tiny image.
#
# Stage 1: Vite bundle for the React dashboard.
FROM node:20-alpine AS web-builder
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci --silent
COPY web/ ./
RUN npm run build

# Stage 2: Go binary using vendored dependencies (no internet required at compile time).
FROM golang:1.26-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY config/ ./config/
RUN CGO_ENABLED=0 GOOS=linux \
    go build -mod=vendor -trimpath -ldflags="-s -w" -o /server ./cmd/server

# Stage 3: Minimal runtime. CA certs come from the go-builder stage so the
# image stays tiny (~15 MB total). Healthcheck for docker-compose uses TCP
# probe instead of wget since scratch has no shell.
FROM scratch
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /app
COPY --from=go-builder /server .
COPY --from=web-builder /web/dist ./web/dist
EXPOSE 8080
CMD ["./server"]
