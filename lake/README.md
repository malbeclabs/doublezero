# Lake

Lake is the data analytics platform for DoubleZero. It provides a web interface and API for querying network telemetry and Solana validator data stored in ClickHouse.

## Components

### api/

HTTP API server that powers the web UI. Provides endpoints for:
- SQL query execution against ClickHouse
- AI-powered natural language to SQL generation
- Conversational chat interface for data analysis
- Schema catalog and visualization recommendations

Serves the built web UI as static files in production.

### web/

React/TypeScript single-page application. Features:
- SQL editor with syntax highlighting
- Natural language query interface
- Chat mode for conversational data exploration
- Query results with tables and charts
- Session history

### agent/

LLM-powered workflow for answering natural language questions. Implements a multi-step process: classify → decompose → generate SQL → execute → synthesize answer. Includes evaluation tests for validating agent accuracy.

See [agent/README.md](agent/README.md) for architecture details.

### indexer/

Background service that continuously syncs data from external sources into ClickHouse:
- Network topology from Solana (DZ programs)
- Latency measurements from Solana (DZ programs)
- Device usage metrics from InfluxDB
- Solana validator data from mainnet
- GeoIP enrichment from MaxMind

See [indexer/README.md](indexer/README.md) for architecture details.

### slack/

Slack bot that provides a chat interface for data queries. Users can ask questions in Slack and receive answers powered by the agent workflow.

### admin/

CLI tool for maintenance operations:
- Database reset
- Data backfills (latency, usage metrics)
- Schema migrations

### migrations/

ClickHouse schema migrations for dimension and fact tables. These are applied automatically by the indexer on startup.

### utils/

Shared Go packages used across lake services (logging, retry logic, test helpers).

## Data Flow

```
External Sources              Lake Services              Storage
────────────────              ─────────────              ───────

Solana (DZ) ───────────────► Indexer ──────────────────► ClickHouse
InfluxDB    ───────────────►    │
MaxMind     ───────────────►    │
                                │
                                ▼
                    ┌───────────────────────┐
                    │      API Server       │◄────── Web UI
                    │  • Query execution    │◄────── Slack Bot
                    │  • Agent workflow     │
                    │  • Chat interface     │
                    └───────────────────────┘
```

## Development

### Local Setup

Run the setup script to get started:

```bash
./scripts/dev-setup.sh
```

This will:
- Start Docker services (ClickHouse, PostgreSQL, Neo4j)
- Create `.env` from `.env.example`
- Download GeoIP databases

Then start the services in separate terminals:

```bash
# Terminal 1: Run the indexer (imports data into ClickHouse)
set -a; . ./.env; set +a
go run indexer/cmd/indexer/main.go --verbose --migrations-enable

# Terminal 2: Run the API server
set -a; . ./.env; set +a
go run ./api/main.go

# Terminal 3: Run the web dev server
cd web
bun install
bun dev
```

The web app will be at http://localhost:5173, API at http://localhost:8080.

### Running Agent Evals

The agent has evaluation tests that validate the natural language to SQL workflow. Run them with:

```bash
./scripts/run-evals.sh                 # Run all evals in parallel
./scripts/run-evals.sh --show-failures # Show failure logs at end
./scripts/run-evals.sh -s              # Short mode (code validation only, no API)
./scripts/run-evals.sh -r 2            # Retry failed tests up to 2 times
```

Output goes to `eval-runs/<timestamp>/` - check `failures.log` for any failures.

## Deployment

Lake uses automated CI/CD via GitHub Actions and ArgoCD.

### Automatic Staging Deploys

Pushes to staging branches automatically build and deploy:
- Build web assets and upload to S3
- Build Docker image and push to `ghcr.io/malbeclabs/doublezero-lake`
- Tag image as `staging` (ArgoCD picks up changes automatically)

Current staging branches are configured in `.github/workflows/release.docker.lake.yml`.

### PR Previews

Add the `preview-lake` label to a PR to trigger a preview build. Assets go to a branch-prefixed location in the preview bucket.

### Promoting to Production

To promote a staging image to production:

**Via GitHub Actions (recommended):**
1. Go to Actions → "promote.lake" workflow
2. Run workflow with source_tag=`staging` and target_tag=`prod`

**Via CLI:**
```bash
./scripts/promote-to-prod.sh           # staging → prod (prompts for confirmation)
./scripts/promote-to-prod.sh -n        # dry-run, show what would happen
./scripts/promote-to-prod.sh -y        # skip confirmation
./scripts/promote-to-prod.sh main prod # promote specific tag
```

ArgoCD will automatically sync the new image.

### Static Asset Fallback

The API server fetches missing static assets from S3 to handle rolling deployments gracefully. When users have cached HTML referencing old JS/CSS bundles, the API fetches those assets from S3 instead of returning 404s.

Configure with:
```bash
ASSET_BUCKET_URL=https://my-bucket.s3.amazonaws.com/assets
```

## Environment

Key dependencies:
- **ClickHouse** - Analytics database
- **Anthropic API** - LLM for natural language features
- **InfluxDB** (optional) - Device usage metrics source
- **MaxMind GeoIP** - IP geolocation databases

## Authentication

Lake supports user authentication with usage limits to control API costs.

### User Tiers

| Tier | Auth Method | Daily Limit |
|------|-------------|-------------|
| Domain users | Google OAuth (allowed domains) | Unlimited |
| Wallet users | Solana wallet (SIWS) | 50 questions |
| Anonymous | IP-based | 5 questions |

Limits reset daily at midnight UTC.

### Configuration

**Backend (API):**

```bash
# Google OAuth - Client ID for verifying ID tokens
GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com

# Email domains that get unlimited access (comma-separated)
AUTH_ALLOWED_DOMAINS=doublezero.xyz,malbeclabs.com
```

**Frontend (Web):**

```bash
# Google OAuth - Client ID for Sign-In button (not sensitive, safe to commit)
VITE_GOOGLE_CLIENT_ID=your-client-id.apps.googleusercontent.com
```

### Setup

1. Create a Google OAuth 2.0 Client ID in [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
2. Add authorized JavaScript origins (e.g., `http://localhost:5173`, `http://localhost`, `https://your-domain.com`)
3. Set the same Client ID in both `GOOGLE_CLIENT_ID` (backend) and `VITE_GOOGLE_CLIENT_ID` (frontend)
4. Configure `AUTH_ALLOWED_DOMAINS` for unlimited access domains

### Global Usage Limits

To set a global daily limit across all users (safety cap for cost control):

```bash
# Optional: Maximum questions per day across all users (0 or unset = unlimited)
# When hit, anonymous and wallet users are blocked; domain users can continue
USAGE_GLOBAL_DAILY_LIMIT=10000

# Optional: Kill switch to block ALL users (for emergencies)
# Set to "1", "true", or "on" to enable
USAGE_KILL_SWITCH=0
```

**Behavior when limits are hit:**

| Scenario | Domain Users | Wallet/Anonymous Users |
|----------|--------------|------------------------|
| Global limit reached | Can continue | Blocked: "Service is currently at capacity" |
| Kill switch enabled | Blocked | Blocked: "Service temporarily unavailable" |

Log warnings are emitted at 50%, 80%, and 100% of the global limit:
```
WARN Global usage threshold crossed threshold_percent=50 current_usage=5000 daily_limit=10000
```

### Prometheus Metrics

Usage metrics are exposed at `/metrics`:

| Metric | Type | Description |
|--------|------|-------------|
| `doublezero_lake_api_usage_questions_total` | Counter | Total questions by account type |
| `doublezero_lake_api_usage_questions_daily` | Gauge | Questions today (resets at midnight UTC) |
| `doublezero_lake_api_usage_tokens_total` | Counter | Tokens used by type and account |
| `doublezero_lake_api_usage_global_limit` | Gauge | Configured global daily limit |
| `doublezero_lake_api_usage_global_utilization` | Gauge | Current utilization (0-1) |
