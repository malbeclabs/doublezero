# Lake Analysis Pipeline Agent

A multi-step LLM-powered pipeline for answering natural language questions about DoubleZero network and Solana validator data.

## Overview

The analysis pipeline transforms natural language questions into SQL queries, executes them against ClickHouse, and synthesizes the results into comprehensive answers. Unlike a ReAct-style agent that loops until done, this pipeline uses discrete, well-defined steps for predictability and debuggability.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              User Question                                  │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │
                                  ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         CLASSIFY (Pre-step)                                 │
│  Determines how to route the question:                                      │
│  • data_analysis → full pipeline                                            │
│  • conversational → direct response (no data query)                         │
│  • out_of_scope → polite rejection                                          │
└─────────────────────────────────┬───────────────────────────────────────────┘
                                  │
            ┌─────────────────────┼─────────────────────┐
            │                     │                     │
            ▼                     ▼                     ▼
     ┌──────────┐          ┌──────────┐          ┌──────────┐
     │out_of_   │          │conversa- │          │  data_   │
     │scope     │          │tional    │          │ analysis │
     └────┬─────┘          └────┬─────┘          └────┬─────┘
          │                     │                     │
          ▼                     ▼                     │
    Direct message          RESPOND                   │
    (capabilities)       (uses history)               │
          │                     │                     │
          └──────────┬──────────┘                     │
                     │                                │
                     │                                ▼
                     │         ┌──────────────────────────────────────────┐
                     │         │              DECOMPOSE (Step 1)          │
                     │         │  Breaks question into data questions     │
                     │         └─────────────────┬────────────────────────┘
                     │                           │
                     │                           ▼
                     │         ┌──────────────────────────────────────────┐
                     │         │           GENERATE (Step 2)              │
                     │         │  Creates SQL for each data question      │
                     │         │  (runs in parallel)                      │
                     │         └─────────────────┬────────────────────────┘
                     │                           │
                     │                           ▼
                     │         ┌──────────────────────────────────────────┐
                     │         │            EXECUTE (Step 3)              │
                     │         │  Runs SQL against ClickHouse             │
                     │         │  with retry and zero-row analysis        │
                     │         └─────────────────┬────────────────────────┘
                     │                           │
                     │                           ▼
                     │         ┌──────────────────────────────────────────┐
                     │         │           SYNTHESIZE (Step 4)            │
                     │         │  Combines results into cited answer      │
                     │         └─────────────────┬────────────────────────┘
                     │                           │
                     └───────────────────────────┤
                                                 ▼
                              ┌───────────────────────────────────────────┐
                              │               Final Answer                │
                              └───────────────────────────────────────────┘
```

## Pipeline Steps

### Pre-step: Classify

Routes questions to the appropriate handler:

| Classification | Description | Handler |
|---------------|-------------|---------|
| `data_analysis` | Questions requiring database queries | Full pipeline |
| `conversational` | Follow-ups, clarifications, capabilities | Direct LLM response |
| `out_of_scope` | Unrelated questions | Polite rejection |

### Step 1: Decompose

Breaks a complex user question into specific, queryable data questions.

- Domain terminology mapping (e.g., "active" → `status = 'activated'`)
- Multi-faceted breakdown (e.g., "network health" → device status, link status, latency)
- Comparison awareness (current vs historical queries)

### Step 2: Generate

Creates SQL queries for each data question using dynamic schema context.

- Fetches current table/column info from ClickHouse
- Includes sample enum values from actual data
- Handles ClickHouse-specific syntax

### Step 3: Execute

Runs SQL queries against ClickHouse with intelligent error recovery.

- Parallel execution of all data questions
- Up to 4 retries with error context for regeneration
- Zero-row analysis: detects suspicious empty results and regenerates

### Step 4: Synthesize

Combines query results into a coherent, cited answer.

- Confidence tracking (HIGH/MEDIUM/LOW based on query success)
- Citation format: `[Q1]`, `[Q2]` references to data sources
- Anomaly highlighting for concerning values

## Domain Knowledge

The pipeline includes domain context for:

- **Network**: Devices, links (WAN/DZX), metros, contributors
- **Users**: Multicast (`kind = 'multicast'`), unicast (`kind = 'ibrl'`), edge filtering
- **Solana**: Validators joined via `dz_users.dz_ip = solana_gossip_nodes.gossip_ip`
- **Status values**: `pending`, `activated`, `suspended`, `deleted`, `rejected`, `drained`

## Design Decisions

### Why a Pipeline Instead of ReAct?

1. **Predictability**: Fixed steps mean consistent latency and cost
2. **Debuggability**: Each step's output is inspectable
3. **Parallelization**: Data questions execute concurrently
4. **Separation of concerns**: Each step has a single responsibility

### Why Dynamic Schema?

- Schemas evolve; static schema would require redeployment
- Sample values help the LLM use correct enum values
- View definitions provide query hints

### Why Zero-Row Analysis?

- Empty results are often caused by incorrect filter values
- LLM can reason about whether zero rows is expected
- Automatic regeneration improves success rate
