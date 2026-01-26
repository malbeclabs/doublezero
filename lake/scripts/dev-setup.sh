#!/bin/bash
set -euo pipefail

# Set up local development environment
#
# Usage:
#   ./scripts/dev-setup.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

echo "=== Lake Dev Setup ==="
echo ""

# Step 1: Start Docker services
echo "=== Starting Docker services ==="
docker compose up -d
docker compose ps
echo ""

# Step 2: Copy .env.example to .env if needed
if [[ ! -f .env ]]; then
    echo "=== Creating .env from .env.example ==="
    cp .env.example .env
    echo "Created .env - you may want to edit it with your local settings"
else
    echo "=== .env already exists, skipping ==="
fi
echo ""

# Step 3: Download and extract GeoIP databases
echo "=== Setting up GeoIP databases ==="
GEOIP_DIR="$ROOT_DIR/.tmp/geoip"
mkdir -p "$GEOIP_DIR"

GEOIP_BASE_URL="https://malbeclabs-dev-public-artifacts.s3.us-east-1.amazonaws.com/geoip"

for db in GeoLite2-ASN GeoLite2-City; do
    if [[ -f "$GEOIP_DIR/${db}.mmdb" ]]; then
        echo "${db}.mmdb already exists, skipping"
    else
        echo "Downloading ${db}..."
        curl -fsSL "${GEOIP_BASE_URL}/${db}.tar.gz" | tar -xzf - -C "$GEOIP_DIR"
        echo "Extracted ${db}.mmdb"
    fi
done

echo ""
echo "GeoIP databases installed to: $GEOIP_DIR"
echo "Update your .env with:"
echo "  GEOIP_CITY_DB_PATH=$GEOIP_DIR/GeoLite2-City.mmdb"
echo "  GEOIP_ASN_DB_PATH=$GEOIP_DIR/GeoLite2-ASN.mmdb"
echo ""

# Step 4: Check for bun
echo "=== Checking dependencies ==="
if ! command -v bun &> /dev/null; then
    echo "bun is not installed. Install it with:"
    echo "  curl -fsSL https://bun.sh/install | bash"
    echo ""
else
    echo "bun: $(bun --version)"
fi

if ! command -v go &> /dev/null; then
    echo "go is not installed. Install it from https://go.dev/dl/"
    echo ""
else
    echo "go: $(go version | awk '{print $3}')"
fi
echo ""

# Step 5: Print next steps
echo "=== Setup complete! ==="
echo ""
echo "Next steps:"
echo ""
echo "1. Run the indexer (imports data into ClickHouse):"
echo "   set -a; . ./.env; set +a"
echo "   go run indexer/cmd/indexer/main.go --verbose --migrations-enable"
echo ""
echo "2. Run the API server (in a separate terminal):"
echo "   set -a; . ./.env; set +a"
echo "   go run ./api/main.go"
echo ""
echo "3. Run the web dev server (in a separate terminal):"
echo "   cd web"
echo "   bun install"
echo "   bun dev"
echo ""
echo "The web app will be available at http://localhost:5173"
echo "The API will be available at http://localhost:8080"
