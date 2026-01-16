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

LLM-powered pipeline for answering natural language questions. Implements a multi-step process: classify → decompose → generate SQL → execute → synthesize answer. Includes evaluation tests for validating pipeline accuracy.

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

Slack bot that provides a chat interface for data queries. Users can ask questions in Slack and receive answers powered by the agent pipeline.

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
                    │  • Agent pipeline     │
                    │  • Chat interface     │
                    └───────────────────────┘
```

## Development

### Running Agent Evals

The agent has evaluation tests that validate the natural language to SQL pipeline. Run them with:

```bash
./scripts/run-evals.sh                 # Run all evals in parallel
./scripts/run-evals.sh --show-failures # Show failure logs at end
./scripts/run-evals.sh -s              # Short mode (code validation only, no API)
./scripts/run-evals.sh -r 2            # Retry failed tests up to 2 times
```

Output goes to `eval-runs/<timestamp>/` - check `failures.log` for any failures.

## Environment

Key dependencies:
- **ClickHouse** - Analytics database
- **Anthropic API** - LLM for natural language features
- **InfluxDB** (optional) - Device usage metrics source
- **MaxMind GeoIP** - IP geolocation databases
