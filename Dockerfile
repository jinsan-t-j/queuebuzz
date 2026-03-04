# ---- Build stage ----
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install ca-certificates for HTTPS calls (FCM, Resend, MongoDB Atlas)
RUN apk add --no-cache ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/queuebuzz ./cmd/main.go

# ---- Runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/queuebuzz /queuebuzz

# Run as non-root user (provided by distroless nonroot image)
USER nonroot:nonroot

EXPOSE 8080

ENTRYPOINT ["/queuebuzz"]
