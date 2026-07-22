#!/bin/bash
set -e

# Change directory to script location
APP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$APP_DIR"

echo "🚀 Starting QueueBuzz deployment process..."

# 1. Refresh application environment variables from OCI Vault (with .env.example fallback)
if [ -f /usr/local/bin/fetch-vault-secrets ]; then
  echo "🔑 Fetching latest secrets from OCI Vault..."
  /usr/local/bin/fetch-vault-secrets || true
fi

if [ ! -f .env ]; then
  echo "⚠️ Creating default .env from .env.example..."
  cp .env.example .env
fi

# 2. Login to GHCR if credentials are provided
if [ -n "$GHCR_TOKEN" ] && [ -n "$GHCR_USER" ]; then
  echo "Logging into GHCR ($GHCR_USER)..."
  echo "$GHCR_TOKEN" | docker login ghcr.io -u "$GHCR_USER" --password-stdin
fi

# 3. Set default APP_IMAGE if not set
export APP_IMAGE="${APP_IMAGE:-ghcr.io/jinsan-t-j/queuebuzz:latest}"

echo "📦 Pulling latest image ($APP_IMAGE)..."
if ! docker compose pull; then
  echo "⚠️ Pull from registry skipped or unauthorized. Building image locally..."
  docker compose build app
fi

echo "⚡ Launching containers with Docker Compose..."
docker compose up -d --remove-orphans

echo "🧹 Pruning unused images..."
docker image prune -f || true

echo "✅ QueueBuzz services started successfully!"
