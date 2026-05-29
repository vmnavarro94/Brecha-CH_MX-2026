# Build the frontend first (locally or in CI):
#   cd web && npm ci && npm run build
#
# Then build the image:
#   docker build -t brecha .

# Stage 1: Build Go binary using vendored dependencies (no internet required)
FROM golang:1.26-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -mod=vendor -trimpath -ldflags="-s -w" -o /server ./cmd/server

# Stage 2: Minimal final image
# CA certs are required for TLS connections to exchange WebSocket streams.
FROM scratch
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /app
COPY --from=go-builder /server .
# web/dist must be pre-built before running docker build
COPY web/dist ./web/dist
EXPOSE 8080
CMD ["./server"]
