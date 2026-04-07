# ---- Build stage ----
FROM golang:1.25-alpine AS builder

# Set build environment
ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOPROXY=https://proxy.golang.org,direct

WORKDIR /app

# Install build dependencies (ca-certs for HTTPS, tzdata for time support)
RUN apk add --no-cache ca-certificates tzdata

# Cache dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Build optimized static binary
# -s -w removes debug info and symbol table to reduce binary size
# -trimpath removes file system paths from the binary for security and reproducibility
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /app/queuebuzz ./main.go

# ---- Runtime stage ----
# distroless/static is minimal, secure, and contains only the essentials
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# Copy necessary files from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /app/queuebuzz .
COPY --from=builder /app/docs ./docs

# Environment defaults for distroless
ENV TZ=UTC

# Run as non-root user provided by distroless
USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/app/queuebuzz"]
