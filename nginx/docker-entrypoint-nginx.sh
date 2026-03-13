#!/bin/sh
set -e

# ── Validate ──────────────────────────────────────────────────────────────────
if [ -z "$DOMAIN" ]; then
  echo "ERROR: DOMAIN is not set in .env" >&2
  echo "  Add:  DOMAIN=yourdomain.com" >&2
  exit 1
fi

# ── Choose config based on SSL_ENABLED ────────────────────────────────────────
if [ "$SSL_ENABLED" = "true" ]; then
  echo "nginx: SSL mode — domain: $DOMAIN"
  # Verify certs exist before starting
  CERT="/etc/letsencrypt/live/$DOMAIN/fullchain.pem"
  if [ ! -f "$CERT" ]; then
    echo "ERROR: Certificate not found at $CERT" >&2
    echo "  Run ./setup-ssl.sh $DOMAIN <email> to obtain a certificate first." >&2
    exit 1
  fi
  sed "s/DOMAIN_PLACEHOLDER/$DOMAIN/g" /etc/nginx/nginx.ssl.template > /etc/nginx/nginx.conf
else
  echo "nginx: HTTP-only mode — domain: $DOMAIN (set SSL_ENABLED=true to enable HTTPS)"
  sed "s/DOMAIN_PLACEHOLDER/$DOMAIN/g" /etc/nginx/nginx.http.template > /etc/nginx/nginx.conf
fi

exec "$@"
