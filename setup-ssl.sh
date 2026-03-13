#!/bin/bash
set -e

# ── Agora SSL Setup ───────────────────────────────────────────────────────────
# Usage: ./setup-ssl.sh yourdomain.com your@email.com
#
# This script obtains a Let's Encrypt certificate for your domain.
# After it completes, set SSL_ENABLED=true in your .env and restart.

DOMAIN="${1}"
EMAIL="${2}"

if [ -z "$DOMAIN" ] || [ -z "$EMAIL" ]; then
  echo "Usage: $0 <domain> <email>"
  echo "  Example: $0 social.example.com admin@example.com"
  exit 1
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Agora SSL Setup"
echo "  Domain: $DOMAIN"
echo "  Email:  $EMAIL"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# ── Step 1: Create required directories ───────────────────────────────────────
echo "→ Creating directories..."
mkdir -p data/certbot/www data/certbot/conf data/uploads
echo "  Done."

# ── Step 2: Start a temporary HTTP-only nginx to serve ACME challenge ─────────
echo "→ Starting temporary HTTP server for ACME challenge..."
docker run --rm -d \
  --name agora-certbot-tmp \
  -p 80:80 \
  -v "$(pwd)/data/certbot/www:/var/www/certbot:ro" \
  nginx:1.25-alpine \
  sh -c 'echo "server { listen 80; location /.well-known/acme-challenge/ { root /var/www/certbot; } location / { return 200 \"ok\"; } }" > /etc/nginx/conf.d/default.conf && nginx -g "daemon off;"'

sleep 2

# ── Step 3: Obtain certificate ────────────────────────────────────────────────
echo "→ Running certbot..."
docker run --rm \
  -v "$(pwd)/data/certbot/www:/var/www/certbot" \
  -v "$(pwd)/data/certbot/conf:/etc/letsencrypt" \
  certbot/certbot certonly \
    --webroot \
    --webroot-path=/var/www/certbot \
    --email "$EMAIL" \
    --agree-tos \
    --no-eff-email \
    --keep-until-expiring \
    -d "$DOMAIN"

CERTBOT_EXIT=$?

# ── Step 4: Stop temporary nginx ──────────────────────────────────────────────
docker stop agora-certbot-tmp 2>/dev/null || true

if [ $CERTBOT_EXIT -ne 0 ]; then
  echo ""
  echo "✗ Certbot failed. Make sure:"
  echo "  1. DNS for $DOMAIN points to this server's IP"
  echo "  2. Port 80 is open and reachable from the internet"
  echo "  3. No other process is using port 80"
  exit 1
fi

# ── Step 5: Verify ────────────────────────────────────────────────────────────
if [ ! -f "data/certbot/conf/live/$DOMAIN/fullchain.pem" ]; then
  echo "✗ Certificate files not found. Something went wrong."
  exit 1
fi

# ── Step 6: Update .env ───────────────────────────────────────────────────────
if [ -f ".env" ]; then
  # Update DOMAIN and SSL_ENABLED if they exist, otherwise append them
  if grep -q "^DOMAIN=" .env; then
    sed -i "s|^DOMAIN=.*|DOMAIN=$DOMAIN|" .env
  else
    echo "DOMAIN=$DOMAIN" >> .env
  fi

  if grep -q "^SSL_ENABLED=" .env; then
    sed -i "s|^SSL_ENABLED=.*|SSL_ENABLED=true|" .env
  else
    echo "SSL_ENABLED=true" >> .env
  fi

  if grep -q "^INSTANCE_DOMAIN=" .env; then
    sed -i "s|^INSTANCE_DOMAIN=.*|INSTANCE_DOMAIN=https://$DOMAIN|" .env
  else
    echo "INSTANCE_DOMAIN=https://$DOMAIN" >> .env
  fi

  echo "→ Updated .env with DOMAIN, SSL_ENABLED=true, INSTANCE_DOMAIN."
else
  echo ""
  echo "  Note: No .env file found. Create one from .env.example and set:"
  echo "    DOMAIN=$DOMAIN"
  echo "    SSL_ENABLED=true"
  echo "    INSTANCE_DOMAIN=https://$DOMAIN"
fi

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  ✓ SSL certificate obtained!"
echo ""
echo "  Start (or restart) Agora:"
echo "    docker compose up -d --build"
echo ""
echo "  Certificates: ./data/certbot/conf/live/$DOMAIN/"
echo "  Renewal is automatic via the certbot container."
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
