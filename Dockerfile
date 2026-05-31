# Workflow:
#   1. cd web && npm install && npm run build   (genera web/dist/)
#   2. docker build -t brecha .                  (o docker compose up --build)
#   3. (opcional) git add web/dist && git commit  para que fly deploy lo use
#
# web/dist está tracked en git para que fly deploy / docker build remotos
# no necesiten correr npm ci adentro del container (evita el bug
# "Exit handler never called!" de npm en redes inestables).

# Stage 1: Go binary using vendored dependencies (no internet required at compile time).
FROM golang:1.26-alpine AS go-builder
WORKDIR /app
COPY go.mod go.sum ./
COPY vendor/ ./vendor/
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY config/ ./config/
RUN CGO_ENABLED=0 GOOS=linux \
    go build -mod=vendor -trimpath -ldflags="-s -w" -o /server ./cmd/server

# Stage 2: Minimal runtime. CA certs come from the go-builder stage so the
# image stays tiny (~5 MB total).
FROM scratch
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
WORKDIR /app
COPY --from=go-builder /server .
# web/dist must be pre-built and committed before docker build / fly deploy.
COPY web/dist ./web/dist
EXPOSE 8080
CMD ["./server"]
