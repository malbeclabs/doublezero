# Claude Code Guidelines

## Project Overview

Lake is a data analytics platform for DoubleZero with a Go API backend and React/TypeScript frontend. Data is stored in ClickHouse.

## Structure

- `api/` - Go HTTP server (chi router, :8080)
- `web/` - React frontend (Vite + Bun, :5173)
- `agent/` - AI SQL generation agent with evals
- `slack/` - Slack bot (user-managed)
- `indexer/` - Data indexing service (user-managed)

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

## Conventions

- TypeScript strict mode - `tsc -b` must pass before builds
- React functional components with hooks
- Tailwind CSS v4 for styling
- API client functions in `web/src/lib/api.ts`
- Go handlers in `api/handlers/`

## Git Commits

- Do NOT include "Co-Authored-By" lines in commit messages
