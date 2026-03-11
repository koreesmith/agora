#!/bin/bash
set -e

# ── Agora SSL Setup ───────────────────────────────────────────────────────────
# Usage: ./setup-ssl.sh yourdomain.com your@email.com

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

# ── Step 1: Patch nginx config with the domain ────────────────────────────────
echo "→ Configuring nginx for $DOMAIN..."
sed "s/DOMAIN_PLACEHOLDER/$DOMAIN/g" nginx/nginx.conf > nginx/nginx.conf.tmp
mv nginx/nginx.conf.tmp nginx/nginx.conf
echo "  Done."

# ── Step 2: Create required directories ───────────────────────────────────────
echo "→ Creating directories..."
mkdir -p data/certbot/www data/certbot/conf data/uploads
echo "  Done."

# ── Step 3: Start a temporary nginx on HTTP only to serve ACME challenge ──────
# We need HTTP running before certbot can verify domain ownership.
echo "→ Starting temporary HTTP-only nginx for ACME challenge..."
docker run --rm -d \
  --name agora-certbot-nginx \
  -p 80:80 \
  -v "$(pwd)/data/certbot/www:/var/www/certbot:ro" \
  nginx:1.25-alpine \
  sh -c 'echo "server { listen 80; location /.well-known/acme-challenge/ { root /var/www/certbot; } location / { return 200 \"ok\"; } }" > /etc/nginx/conf.d/default.conf && nginx -g "daemon off;"'

sleep 2

# ── Step 4: Run certbot ───────────────────────────────────────────────────────
echo "→ Running certbot to obtain certificate..."
docker run --rm \
  -v "$(pwd)/data/certbot/www:/var/www/certbot" \
  -v "$(pwd)/data/certbot/conf:/etc/letsencrypt" \
  certbot/certbot certonly \
    --webroot \
    --webroot-path=/var/www/certbot \
    --email "$EMAIL" \
    --agree-tos \
    --no-eff-email \
    -d "$DOMAIN"

CERTBOT_EXIT=$?

# ── Step 5: Stop temporary nginx ─────────────────────────────────────────────
docker stop agora-certbot-nginx 2>/dev/null || true

if [ $CERTBOT_EXIT -ne 0 ]; then
  echo ""
  echo "✗ Certbot failed. Make sure:"
  echo "  1. DNS for $DOMAIN points to this server's IP"
  echo "  2. Port 80 is open and reachable from the internet"
  echo "  3. No other process is using port 80"
  exit 1
fi

echo "  Certificate obtained successfully."

# ── Step 6: Create a symlink so nginx.conf path works ────────────────────────
# Certbot stores certs at /etc/letsencrypt/live/<domain>/
# Our volume maps data/certbot/conf → /etc/letsencrypt
echo "→ Verifying certificate files..."
if [ ! -f "data/certbot/conf/live/$DOMAIN/fullchain.pem" ]; then
  echo "✗ Certificate files not found at expected path."
  exit 1
fi
echo "  Found: data/certbot/conf/live/$DOMAIN/"

# ── Step 7: Write docker-compose.ssl.yml ──────────────────────────────────────
echo "→ Writing docker-compose.ssl.yml..."
cat > docker-compose.ssl.yml << YAML
# SSL-enabled compose file — use instead of docker-compose.yml
# Start with: docker compose -f docker-compose.ssl.yml up -d

services:

  postgres:
    image: postgres:16-alpine
    container_name: agora-postgres
    restart: unless-stopped
    user: "500:500"
    environment:
      POSTGRES_DB:       agora
      POSTGRES_USER:     agora
      POSTGRES_PASSWORD: agora
    volumes:
      - ./data/postgres:/var/lib/postgresql/data
    networks:
      - agora
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U agora"]
      interval: 5s
      timeout: 3s
      retries: 10

  redis:
    image: redis:7-alpine
    container_name: agora-redis
    restart: unless-stopped
    command: redis-server --appendonly yes
    volumes:
      - ./data/redis:/data
    networks:
      - agora
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 10

  backend:
    build:
      context: .
      dockerfile: Dockerfile
    container_name: agora-backend
    restart: unless-stopped
    expose:
      - "8080"
    env_file:
      - .env
    environment:
      - HTTP_ADDR=:8080
      - DATABASE_URL=postgres://agora:agora@postgres:5432/agora?sslmode=disable
      - REDIS_URL=redis://redis:6379
    volumes:
      - ./data/uploads:/data/uploads
    networks:
      - agora
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy

  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile.frontend
    container_name: agora-frontend
    restart: unless-stopped
    expose:
      - "80"
    volumes:
      - ./data/uploads:/data/uploads:ro
    networks:
      - agora
    depends_on:
      - backend

  # ── SSL-terminating nginx ──────────────────────────────────────────────────
  nginx:
    build:
      context: ./nginx
      dockerfile: Dockerfile.nginx
    container_name: agora-nginx
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./data/certbot/conf:/etc/letsencrypt:ro
      - ./data/certbot/www:/var/www/certbot:ro
      - ./data/uploads:/data/uploads:ro
    networks:
      - agora
    depends_on:
      - frontend

  # ── Certbot auto-renewal ───────────────────────────────────────────────────
  certbot:
    image: certbot/certbot
    container_name: agora-certbot
    restart: unless-stopped
    volumes:
      - ./data/certbot/conf:/etc/letsencrypt
      - ./data/certbot/www:/var/www/certbot
    # Renew twice daily; certbot only acts when cert is within 30 days of expiry
    entrypoint: /bin/sh -c "trap exit TERM; while :; do certbot renew --webroot --webroot-path=/var/www/certbot --quiet; sleep 12h & wait \$\${!}; done"
    networks:
      - agora

networks:
  agora:
    driver: bridge
YAML

echo "  Written."

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  ✓ SSL setup complete!"
echo ""
echo "  Start Agora with SSL:"
echo "    docker compose -f docker-compose.ssl.yml up -d"
echo ""
echo "  Certificates stored in: ./data/certbot/conf/"
echo "  Renewal is automatic (certbot container checks every 12h)"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
