#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────────
# init-ssl.sh — One-time SSL certificate bootstrap
#
# Usage:
#   ./init-ssl.sh yourdomain.com admin@yourdomain.com
#
# What it does:
#   1. Validates that the stack is running (Nginx on HTTP)
#   2. Requests a Let's Encrypt certificate via Certbot
#   3. Replaces nginx.conf with the HTTPS-enabled nginx.ssl.conf
#   4. Reloads Nginx to activate HTTPS
#
# After this script runs once, Certbot auto-renews via the
# renewal loop defined in docker-compose.yml.
# ──────────────────────────────────────────────────────────────
set -euo pipefail

DOMAIN="${1:?Usage: $0 <domain> <email>}"
EMAIL="${2:?Usage: $0 <domain> <email>}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
NGINX_DIR="${SCRIPT_DIR}/nginx"

echo "╔══════════════════════════════════════════╗"
echo "║  QueueBuzz SSL Certificate Bootstrap     ║"
echo "╠══════════════════════════════════════════╣"
echo "║  Domain: ${DOMAIN}"
echo "║  Email:  ${EMAIL}"
echo "╚══════════════════════════════════════════╝"
echo ""

# ─── Step 1: Verify the stack is running ───
echo "▸ Step 1: Checking that Nginx is running on HTTP..."
if ! docker compose ps --status running | grep -q nginx; then
    echo "✗ Nginx is not running. Start the stack first:"
    echo "  docker compose up -d --scale app=2"
    exit 1
fi
echo "  ✓ Nginx is running"
echo ""

# ─── Step 2: Verify ACME challenge path is reachable ───
echo "▸ Step 2: Verifying ACME challenge path..."
mkdir -p "${NGINX_DIR}/certbot/www/.well-known/acme-challenge"
echo "acme-test" > "${NGINX_DIR}/certbot/www/.well-known/acme-challenge/test"
echo "  ✓ ACME challenge directory prepared"
echo ""

# ─── Step 3: Request certificate from Let's Encrypt ───
echo "▸ Step 3: Requesting SSL certificate from Let's Encrypt..."
docker compose run --rm certbot certonly \
    --webroot \
    --webroot-path=/var/www/certbot \
    --email "${EMAIL}" \
    --agree-tos \
    --no-eff-email \
    --force-renewal \
    -d "${DOMAIN}"

if [ $? -ne 0 ]; then
    echo "✗ Certbot failed. Ensure your domain's DNS A record points to this server."
    exit 1
fi
echo "  ✓ Certificate obtained for ${DOMAIN}"
echo ""

# ─── Step 4: Generate HTTPS Nginx config from template ───
echo "▸ Step 4: Generating HTTPS Nginx config..."
if [ ! -f "${NGINX_DIR}/nginx.ssl.conf" ]; then
    echo "✗ nginx.ssl.conf template not found at ${NGINX_DIR}/nginx.ssl.conf"
    exit 1
fi

# Replace __DOMAIN__ placeholder with actual domain
sed "s/__DOMAIN__/${DOMAIN}/g" "${NGINX_DIR}/nginx.ssl.conf" > "${NGINX_DIR}/nginx.conf"
echo "  ✓ nginx.conf updated with HTTPS config for ${DOMAIN}"
echo ""

# ─── Step 5: Reload Nginx ───
echo "▸ Step 5: Reloading Nginx..."
docker compose exec nginx nginx -t
docker compose exec nginx nginx -s reload
echo "  ✓ Nginx reloaded with HTTPS"
echo ""

# ─── Cleanup ───
rm -f "${NGINX_DIR}/certbot/www/.well-known/acme-challenge/test"

echo "══════════════════════════════════════════"
echo "  ✓ HTTPS is now active for ${DOMAIN}"
echo ""
echo "  Verify: curl -I https://${DOMAIN}/healthcheck"
echo ""
echo "  Certbot will auto-renew via the renewal"
echo "  loop in docker-compose.yml (every 12h)."
echo "══════════════════════════════════════════"
