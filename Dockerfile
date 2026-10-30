# syntax=docker/dockerfile:1

# ---- bundle telemetri browser (OpenTelemetry Web) ----
FROM node:24.22.2-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/src ./src
RUN npm run build

# ---- build Go ----
FROM golang:1.27.3-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# otel.js harus ada sebelum go build karena di-embed ke binary.
COPY --from=web /web/dist/otel.js internal/templates/static/otel.js

# Binary statis (tanpa cgo) agar bisa berjalan di image distroless/static.
ENV CGO_ENABLED=0 GOOS=linux
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server && \
    go build -trimpath -ldflags="-s -w" -o /out/create-user ./cmd/create-user && \
    go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# ---- runtime ----
# distroless/static: tanpa shell dan package manager; sudah berisi CA
# certificates dan tzdata. Tag :nonroot menjalankan proses sebagai UID 65532.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/server"]
