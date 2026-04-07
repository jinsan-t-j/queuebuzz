# QueueBuzz Backend

QueueBuzz is a modern, high-performance virtual queue management platform API. It allows hosts to create proximity-based queues and users to join them seamlessly via location verification and WebSocket-driven real-time updates.

## Features

- **Queue Management**: Create queues, manage sizes, set geofences (location radius), and process entries.
- **Host Authentication**: Supports anonymous queues with token-based ownership, as well as registered host accounts using passwordless auth (OTP & Magic Links via Resend).
- **Proximity & Geofencing**: Validates user location before allowing them to join a queue.
- **Real-Time Updates**: WebSocket hub to broadcast queue changes to connected clients instantly.
- **Push Notifications**: Firebase Cloud Messaging (FCM) integration to notify waiting users when it's their turn.
- **Robust Security**: Rate limiting (Redis-backed), RS256 JWT authentication, structured CORS, and strict HTTP security headers.
- **Automatic Expiry**: Redis keyspace listeners and cron fallbacks to automatically clean up expired queues and delete personally identifiable information (PII).

## Tech Stack

- **Go**: v1.25+
- **Framework**: [Fiber v3](https://docs.gofiber.io/)
- **Database**: MongoDB (via `mongo-driver/v2`)
- **Caching & Pub/Sub**: Redis (via `go-redis/v9`)
- **Email**: Resend API
- **Push Notifications**: Firebase Admin SDK
- **Containerization**: Docker (multi-stage to `distroless/static-debian12`)

## Prerequisites

- Go 1.25 or higher
- Docker and Docker Compose (for local database & cache)
- `make` utility
- [Air](https://github.com/cosmtrek/air) (for live reloading during development)

## Getting Started

### 1. Clone the repository

```bash
git clone git@github.com:jinsan-t-j/queuebuzz.git
cd queuebuzz
```

### 2. Environment Configuration

Create a `.env` file in the root directory and populate it based on `internal/config/config.go`:

```env
PORT=8080
APP_ENV=development

# MongoDB
DB_URI=mongodb://localhost:27017
DB_NAME=queuebuzz

# Redis
REDIS_URL=redis://localhost:6379
REDIS_PASSWORD=

# JWT RS256 (PEM encoded strings)
JWT_PRIVATE_KEY="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----"
JWT_PUBLIC_KEY="-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----"

# Firebase (Service Account JSON)
FIREBASE_CREDENTIALS='{"type": "service_account", ...}'

# Resend (Email)
RESEND_API_KEY=re_YOUR_KEY_HERE
EMAIL_FROM=noreply@queuebuzz.com
EMAIL_REPLY_TO=support@queuebuzz.com

# CORS
ALLOWED_ORIGIN=http://localhost:3000
```

### 3. Start Infrastructure

Start the local MongoDB and Redis instances using Docker Compose:

```bash
docker compose up -d mongo redis
```

### 4. Run the Server

You can run the application with hot-reloading using `make`:

```bash
make dev
```

Alternatively, to just build and run:
```bash
make build
./tmp/main
```

## Available Make Commands

- `make dev` - Run the app with `air` hot-reload.
- `make build` - Compile the Go binary to `./tmp/main`.
- `make test` - Run tests with the race detector enabled.
- `make vet` - Run Go linting/vet checks.
- `make swagger` - Generate Swagger documentation files (requires `swag` CLI).
- `make docker` - Build the production Docker image.
- `make clean` - Remove binaries and generated docs.

## API Documentation

The API uses Swagger for documentation. Make sure to generate the docs:

```bash
make swagger
```

## Structure

```text
.
├── cmd/             # Application entry point (main.go)
├── internal/        # Private application code
│   ├── app/         # Application bootstrap & dependency injection
│   ├── config/      # Environment configuration loading
│   ├── constants/   # Application-wide constants
│   ├── db/          # Database connection adapters (Mongo, Redis)
│   ├── handlers/    # HTTP/REST request handlers
│   ├── helpers/     # Standardized responses and utilities
│   ├── log/         # Logging configuration (zerolog)
│   ├── middlewares/ # Fiber middlewares (Auth, Route-limiting, CORS, etc.)
│   ├── models/      # MongoDB structures and domain logic
│   ├── providers/   # Validation logic
│   ├── routes/      # Fiber route registration
│   ├── services/    # Business logic & external service integrations
│   └── ws/          # WebSocket hub / client implementation
├── Dockerfile       # Container definition
├── docker-compose.yml # Local development infrastructure
└── Makefile         # Build tooling
```

## CI/CD

The project includes GitHub Actions workflows mapped in `.github/workflows/`:
- **Lint & Test**: Reusable workflow triggered by both staging and production pipelines. Runs `go vet`, `go build`, and tests.
- **Stage**: Triggers on pushes to the `stage` branch. Builds and pushes the Docker container to GHCR tagged as `stage`.
- **Production**: Triggers on GitHub Releases (`published`). Builds and pushes to GHCR tagged as `latest` and the respective release tag version.
