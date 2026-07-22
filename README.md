# QueueBuzz Backend

QueueBuzz is a modern, high-performance virtual queue management platform API. It allows hosts to create proximity-based queues and users to join them seamlessly via location verification, SSE-driven real-time updates, and automated notifications.

## Features

### 🔐 Authentication & Security

- **Multi-Method Auth**: Passwordless login (Magic Links & OTP), Social Login (Google, Facebook), and Anonymous guest sessions.
- **Token Management**: RS256 JWT implementation with automatic token rotation and refresh logic.
- **Security Hardening**: Rate limiting (Redis-backed), CORS protection, strict security headers, and an active email blocklist to prevent spam.

### 📋 Queue Management

- **Smart Discovery**: Join queues via unique IDs, custom slugs, or simple 6-digit Join Codes.
- **Host Controls**: Call next guest (or specific guest), mark as served, pause/resume/terminate queues.
- **Advanced Ordering**:
  - **Manual Position Reordering**: Drag-and-drop support (via API) for manual guest priority.
  - **Strict Queue Mode**: Lock the queue order to prevent accidental skips.
- **Capacity Management**: Configurable guest limits per session based on host billing tier.
- **Session Notes**: Rich-text or plain-text notes for every active queue session.

### 👤 Customer Experience

- **Position Tracking**: Real-time position and estimated wait time updates.
- **Session Recovery**: Claim guest positions from recovery emails or persistent browser tokens.
- **Presence Verification**: Proximity-aware "Arrived" confirmation and "Still Here" check-ins to prevent ghost entries.
- **Service Completion**: Guests can signal when their service is finished.

### 📊 Analytics & Insights

- **Host Dashboard**: High-level summaries of served today, average wait times, and peak usage hours.
- **Historical Data**: Comprehensive history logs with bulk deletion and cleanup controls.
- **Data Export**: Support for exporting served guest lists to CSV/Excel formats (Premium).

### 💳 Billing & Subscriptions

- **Multi-Tier Plans**: Free, Pro, Elite, and Enterprise tiers with varying limits (Queues, Guests, History).
- **Payment Integration**: Webhook-driven status updates, self-serve checkout URL generation, and subscription cancellation.
- **Renewal Reminders**: Automated background jobs to notify users before subscription expiry.

### 🔔 Notifications & Real-Time

- **Real-Time Streams**: Server-Sent Events (SSE) for both host and guest updates, ensuring zero-latency status changes.
- **Push Notifications**: Firebase Cloud Messaging (FCM) integration for mobile push alerts when it's the guest's turn.
- **Transactional Emails**: Automated recovery, reporting, and account emails via Brevo/Resend.
- **Queue Broadcast**: Hosts can send instant notifications to all waiting guests in an active session.

### 🧹 Maintenance & Cleanup

- **Automatic Expiry**: Redis keyspace listeners and cron fallbacks to close inactive queues.
- **PII Protection**: Automated cleanup jobs to delete personally identifiable information after the retention period expires.

## Tech Stack

- **Go**: v1.25+
- **Framework**: [Fiber v3](https://docs.gofiber.io/)
- **Database**: MongoDB (via `mongo-driver/v2`)
- **Caching & Pub/Sub**: Redis (via `go-redis/v9`)
- **Email**: Brevo API (Transactional)
- **Push Notifications**: Firebase Admin SDK
- **Logging**: Zerolog with structured JSON output
- **Containerization**: Docker (multi-stage to `distroless/static-debian12`)

## Structure

```text
.
├── cmd/                 # Application entry point (main.go)
├── internal/            # Private application code
│   ├── app/             # Application bootstrap, router, & container (DI)
│   ├── config/          # Environment configuration loading
│   ├── constants/       # Global constants & error codes
│   ├── database/        # Connection adapters (Mongo, Redis)
│   ├── exceptions/      # Standardized error handling
│   ├── firebase/        # FCM provider implementation
│   ├── helpers/         # Response utilities & common helpers
│   ├── log/             # Structured logging setup
│   ├── middlewares/     # Auth, Rate-limiting, CORS, Capacity guards
│   ├── modules/         # Feature-based domain logic
│   │   ├── auth/        # Social, OTP, Magic Link auth
│   │   ├── billing/     # Plans, Subscriptions, Webhooks
│   │   ├── customer/    # Guest actions & recovery
│   │   ├── host/        # Host profile & account management
│   │   ├── notification/# FCM, SSE, & Email services
│   │   ├── queue/       # Core queue logic & management
│   │   └── system/      # Health, configuration, & maintenance
│   ├── sse/             # Server-Sent Events hub
│   ├── services/        # Shared cross-module services
│   └── validator/       # Custom request validation logic
├── Dockerfile           # Production container definition
├── docker-compose.yml   # Local infrastructure (Mongo, Redis)
└── Makefile             # Build & development tooling
```

## Getting Started

### 1. Clone the repository

```bash
git clone git@github.com:jinsan-t-j/queuebuzz.git
cd queuebuzz
```

### 2. Environment Configuration

Create a `.env` file based on `.env.example`:

```env
PORT=8080
APP_ENV=development

# Database (Oracle NoSQL / Mongo-compatible interface)
DB_URI=mongodb://localhost:27017
DB_NAME=queuebuzz

# Redis / Valkey
REDIS_URL=redis://localhost:6379
REDIS_PASSWORD=

# JWT RS256 (PEM encoded strings)
JWT_PRIVATE_KEY="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----"
JWT_PUBLIC_KEY="-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----"

# Firebase (Service Account JSON)
FIREBASE_CREDENTIALS='{"type": "service_account", ...}'

# Brevo (Email)
BREVO_API_KEY=xkeysib-YOUR_KEY_HERE
EMAIL_FROM=noreply@queuebuzz.com
```

### 3. Start Infrastructure

```bash
docker compose up -d valkey
```

### 4. Run the Server

```bash
make dev
```

## Available Make Commands

- `make dev` - Run the app with `air` hot-reload.
- `make build` - Compile the Go binary to `./tmp/main`.
- `make test` - Run tests with the race detector.
- `make swagger` - Generate Swagger documentation.
- `make docker` - Build production Docker image.

## API Documentation

The API uses Swagger. Access it at `/swagger/index.html` after generating:

```bash
make swagger
```

## CI/CD

Workflows in `.github/workflows/`:

- **Lint & Test**: Runs on every pull request.
- **Stage**: Deploys to staging on pushes to `stage`.
- **Production**: Deploys to production on tagged releases.
