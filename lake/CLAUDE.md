# Claude Code Guidelines

## Project Overview

Lake is a data analytics platform for the DoubleZero (DZ) network. It ingests network telemetry and Solana validator data into ClickHouse, and provides an AI agent that answers natural language questions by generating and executing SQL queries.

The agent is the core feature - it lets users ask questions like "which validators are on DZ?" or "show network health" and get data-driven answers.

## Structure

- `agent/` - AI SQL generation agent (the main feature)
- `api/` - Go HTTP server (chi router, :8080)
- `web/` - React frontend (Vite + Bun, :5173)
- `indexer/` - Data indexing service (user-managed)
- `slack/` - Slack bot (user-managed)

## Commands

Run API server from lake as working directory:

```bash
cd web && bun run build   # Build frontend (runs tsc first)
go run ./api/main.go      # Run API server
```

### Agent Evals

```bash
cd agent && go test -tags=evals ./evals/... -short  # Code validation only
cd agent && go test -tags=evals ./evals/... -v      # Full evals (hits Anthropic API - confirm first)
```

**Evals are the source of truth for agent quality.** The agent prompts (CLASSIFY, DECOMPOSE, GENERATE, SYNTHESIZE) and evals work together:

- When changing agent prompts or context: evals must continue to pass. If an eval fails, fix the agent behavior, not the expectation.
- When working on evals: the goal is to improve the agent. Add expectations that enforce better behavior, don't weaken expectations to make tests pass.

## Conventions

- TypeScript strict mode - `tsc -b` must pass before builds
- React functional components with hooks
- Tailwind CSS v4 for styling
- API client functions in `web/src/lib/api.ts`
- Go handlers in `api/handlers/`

## Git Commits

- Do NOT include "Co-Authored-By" lines in commit messages
