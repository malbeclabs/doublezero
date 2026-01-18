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

## Service Management

Do NOT manage the `api` or `web` services. The user runs these separately and will restart them as needed.

## Commands

```bash
go run ./api/main.go      # Run API server (:8080)
cd web && bun run dev     # Run web dev server (:5173)
cd web && bun run build   # Build frontend (runs tsc first)
```

### Agent Evals

```bash
./scripts/run-evals.sh                 # Run all Anthropic evals in parallel
./scripts/run-evals.sh --show-failures # Show failure logs at end
./scripts/run-evals.sh -s              # Short mode (code validation only, no API calls)
./scripts/run-evals.sh -r 2            # Retry failed tests up to 2 times
./scripts/run-evals.sh -f 'NetworkHealth'  # Filter to specific tests
```

Output goes to `eval-runs/<timestamp>/` with:
- `failures.log` - All failure output (check this first)
- `flaky.log` - Tests that failed initially but passed on retry (review to identify unstable behavior)
- `successes.log` - All success output
- `<TestName>.log` - Individual test logs

**When to run evals:** Only after changing agent logic (prompts, context, or code in `agent/`). Changes to `api/` or `web/` do not require evals.

**IMPORTANT:** Do not run the full eval suite without asking the user first. Running all evals takes several minutes and costs money. When you need to verify changes, run specific tests with `-f 'TestName'` or use `-s` for short mode. Only run the full suite when the user explicitly requests it.

**Do NOT run OllamaLocal evals.** The OllamaLocal tests skip when Ollama isn't available, which makes them appear to pass. Only run the Anthropic evals (filter with `-f 'Anthropic'` if needed).

**Evals are the source of truth for agent quality.** The agent system prompt and evals work together:

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
