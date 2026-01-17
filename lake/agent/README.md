# Lake Analysis Agent

An LLM-powered agent for answering natural language questions about DoubleZero network and Solana validator data.

## Overview

The agent transforms natural language questions into SQL queries, executes them against ClickHouse, and synthesizes the results into comprehensive answers. It uses a tool-calling workflow where the LLM iteratively reasons about the question and executes queries until it has enough data to answer.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              User Question                                  │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                            Tool-Calling Loop                                │
│                                                                             │
│   ┌─────────────────────────────────────────────────────────────────────┐   │
│   │  LLM with System Prompt + Schema + Conversation History             │   │
│   └─────────────────────────────────────────────────────────────────────┘   │
│                          │                    │                             │
│                          ▼                    ▼                             │
│                    ┌──────────┐        ┌─────────────┐                      │
│                    │  think   │        │ execute_sql │                      │
│                    │          │        │             │                      │
│                    │ Record   │        │ Run queries │                      │
│                    │ reasoning│        │ against DB  │                      │
│                    └──────────┘        └─────────────┘                      │
│                          │                    │                             │
│                          └────────────────────┘                             │
│                                   │                                         │
│                                   ▼                                         │
│                          [Loop until done]                                  │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Final Answer                                   │
│                                                                             │
│  Natural language response with:                                            │
│  • Direct answer to the question                                            │
│  • Citations [Q1], [Q2] referencing specific queries                        │
│  • Caveats and limitations                                                  │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Workflow

The agent follows an iterative workflow guided by the system prompt:

### 1. Interpret
Understand what is actually being asked:
- What type of question? (descriptive, comparative, diagnostic)
- What entities and time windows are implied?
- What would a wrong answer look like?

### 2. Map to Data
Translate to concrete data terms:
- Which tables/views are relevant?
- What is the unit of analysis?
- Are there known caveats or gaps?

### 3. Plan Queries
Outline the query approach:
- Start with small validation queries (row counts, time coverage)
- Separate exploration from answer-producing queries
- Batch independent queries for parallel execution

### 4. Execute
Run queries and assess results:
- Check row counts against intuition
- Look for outliers or suspiciously clean results
- Investigate if results contradict expectations

### 5. Iterate
Refine as needed:
- Adjust filters after seeing real distributions
- Validate that metrics mean what the question assumes
- Only proceed when the pattern is robust

### 6. Synthesize
Turn data into an answer:
- State what the data shows, not what it implies
- Tie each claim to an observed metric
- Quantify uncertainty and blind spots

## Tools

The agent has access to two tools:

| Tool | Purpose |
|------|---------|
| `think` | Record reasoning process (shown to users for transparency) |
| `execute_sql` | Run SQL queries against ClickHouse and get results |

## Question Types

| Type | Handling |
|------|----------|
| **Data Analysis** | Questions requiring SQL queries (e.g., "How many validators are on DZ?") - uses full workflow |
| **Conversational** | Clarifications, capabilities, follow-ups - direct response without queries |
| **Out of Scope** | Unrelated questions - polite redirect |

## Domain Knowledge

The system prompt includes domain context for:

- **Network**: Devices, links (WAN/DZX), metros, contributors
- **Users**: Multicast (`kind = 'multicast'`), unicast (`kind = 'ibrl'`), edge filtering
- **Solana**: Validators joined via `dz_users.dz_ip = solana_gossip_nodes.gossip_ip`
- **Status values**: `pending`, `activated`, `suspended`, `deleted`, `rejected`, `drained`
- **ClickHouse specifics**: NULL vs empty string, quantile syntax, date functions

## Pre-built Views

The schema includes pre-built views that the agent is instructed to prefer:

| View | Use For |
|------|---------|
| `solana_validators_on_dz_current` | Validators currently on DZ |
| `solana_validators_off_dz_current` | Validators NOT on DZ with GeoIP |
| `solana_validators_on_dz_connections` | All connection events |
| `solana_validators_disconnections` | Validators that left DZ |
| `solana_validators_new_connections` | Recently connected validators |
| `dz_links_health_current` | Current link health state |
| `dz_link_status_changes` | Link status history |
| `dz_vs_internet_latency_comparison` | DZ vs public internet latency |

## Design Decisions

### Why Tool-Calling?

1. **Flexibility**: The agent can execute as many queries as needed
2. **Iteration**: Results inform the next step, allowing refinement
3. **Transparency**: The `think` tool makes reasoning visible to users
4. **Natural flow**: Mirrors how a human analyst would work

### Why Dynamic Schema?

- Schemas evolve; static schema would require redeployment
- Sample values help the LLM use correct enum values
- View definitions provide query hints

### Why Claim Attribution?

Every factual claim references its source query (e.g., `[Q1]`):
- Users can trace any claim back to the data
- Builds trust through transparency
- Makes it easy to verify specific numbers
