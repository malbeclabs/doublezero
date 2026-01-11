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
                     │         │  • Domain terminology mapping            │
                     │         │  • Multi-faceted question breakdown      │
                     │         └─────────────────┬────────────────────────┘
                     │                           │
                     │                           ▼
                     │         ┌──────────────────────────────────────────┐
                     │         │           GENERATE (Step 2)              │
                     │         │  Creates SQL for each data question      │
                     │         │  • Dynamic schema injection              │
                     │         │  • Sample values for enums               │
                     │         │  ┌────────────────────────────────────┐  │
                     │         │  │ Runs in PARALLEL for each question │  │
                     │         │  └────────────────────────────────────┘  │
                     │         └─────────────────┬────────────────────────┘
                     │                           │
                     │                           ▼
                     │         ┌──────────────────────────────────────────┐
                     │         │            EXECUTE (Step 3)              │
                     │         │  Runs SQL against ClickHouse             │
                     │         │  • Automatic retry on errors             │
                     │         │  • Zero-row analysis & regeneration      │
                     │         └─────────────────┬────────────────────────┘
                     │                           │
                     │                           ▼
                     │         ┌──────────────────────────────────────────┐
                     │         │           SYNTHESIZE (Step 4)            │
                     │         │  Combines results into final answer      │
                     │         │  • Confidence assessment                 │
                     │         │  • Citation generation [Q1], [Q2]        │
                     │         └─────────────────┬────────────────────────┘
                     │                           │
                     └───────────────────────────┤
                                                 │
                                                 ▼
                              ┌───────────────────────────────────────────┐
                              │               Final Answer                │
                              └───────────────────────────────────────────┘
```

## Pipeline Steps

### Pre-step: Classify

Routes questions to the appropriate handler based on intent:

| Classification | Description | Handler |
|---------------|-------------|---------|
| `data_analysis` | Questions requiring database queries | Full pipeline |
| `conversational` | Follow-ups, clarifications, capabilities | Direct LLM response |
| `out_of_scope` | Unrelated questions | Polite rejection |

**Examples:**
- "How many validators are connected?" → `data_analysis`
- "What do you mean by that?" → `conversational`
- "What's the weather today?" → `out_of_scope`

### Step 1: Decompose

Breaks a complex user question into specific, queryable data questions.

**Input:** Natural language question + conversation history
**Output:** Array of `DataQuestion` with question text and rationale

**Features:**
- Domain terminology mapping (e.g., "active" → `status = 'activated'`)
- Multi-faceted breakdown (e.g., "network health" → device status, link status, latency, errors)
- Comparison awareness (e.g., "validators connected today" → current vs historical)

**Example:**
```
User: "How is the network performing?"

Data Questions:
1. How many devices are in activated status? (baseline operational count)
2. How many links are in activated status? (connectivity health)
3. What is the average and P95 latency across WAN links in the last 24h? (performance)
4. Which links have packet loss > 0.1% in the last 24h? (quality issues)
```

### Step 2: Generate

Creates SQL queries for each data question using dynamic schema context.

**Input:** Data question + live database schema
**Output:** `GeneratedQuery` with SQL and explanation

**Features:**
- **Dynamic schema injection**: Fetches current table/column info from ClickHouse
- **Sample value hints**: Includes actual enum values (e.g., `status` values: activated, pending, suspended)
- **ClickHouse-aware**: Handles ClickHouse-specific syntax and behaviors

### Step 3: Execute

Runs SQL queries against ClickHouse with intelligent error recovery.

**Input:** Generated SQL query
**Output:** `ExecutedQuery` with results or error

**Features:**
- **Parallel execution**: All data questions run concurrently
- **Retry on error**: Up to 4 retries with error context for regeneration
- **Zero-row analysis**: Detects suspicious empty results and regenerates

```
┌─────────────────────────────────────────────────────────────┐
│                    Error Recovery Flow                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Execute Query                                              │
│       │                                                     │
│       ├── Success with rows ──────────────────► Return      │
│       │                                                     │
│       ├── Success with 0 rows                               │
│       │        │                                            │
│       │        ▼                                            │
│       │   Analyze Zero Result                               │
│       │        │                                            │
│       │        ├── Expected (e.g., count=0) ──► Return      │
│       │        │                                            │
│       │        └── Suspicious ──► Regenerate & Retry        │
│       │                                                     │
│       └── Error                                             │
│                │                                            │
│                ▼                                            │
│           Regenerate with error context                     │
│                │                                            │
│                └── Retry (up to 4 times)                    │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

### Step 4: Synthesize

Combines query results into a coherent, cited answer.

**Input:** User question + all executed queries with results
**Output:** Formatted answer with citations

**Features:**
- **Confidence tracking**: HIGH/MEDIUM/LOW based on query success
- **Citation format**: `[Q1]`, `[Q2]` references to data sources
- **Structured output**: Headers, bullet points, appropriate units
- **Anomaly highlighting**: Calls out concerning values

**Example output:**
```
🔌 **Device Status**
- 75 devices activated [Q1]
- 0 devices in other states [Q1]

🔗 **Link Health**
- 128 links activated [Q2]
- 3 links showing packet loss > 0.1% [Q3]:
  - `nyc-lon-1`: 2.5% loss
  - `tok-sgp-1`: 0.8% loss

⚠️ **Attention Required**
- `nyc-lon-1` packet loss elevated from baseline [Q3, Q4]
```

## Domain Knowledge

The pipeline includes extensive domain context in prompts:

### Network Concepts
- **Devices**: Routers/switches in the DZ network
- **Links**: Connections between devices (WAN = inter-metro, DZX = intra-metro)
- **Metros**: Data center locations (NYC, LON, TOK, etc.)
- **Contributors**: Operators who manage devices and links

### User Types
- **Multicast**: `kind = 'multicast'` - receives multicast streams
- **Unicast**: `kind = 'ibrl'` or `'ibrl_with_allocated_ip'`
- **Edge filtering**: `kind = 'edge_filtering'`

### Solana Integration
- **Validators**: Connected via `dz_users.dz_ip = solana_gossip_nodes.gossip_ip`
- **Stake**: `activated_stake_lamports` on vote accounts
- **Vote lag**: `cluster_slot - last_vote_slot`

### Status Values
- `pending`, `activated`, `suspended`, `deleted`, `rejected`, `drained`
- "Active" typically means `status = 'activated'`

## Usage

```go
package main

import (
    "context"
    "github.com/malbeclabs/doublezero/lake/agent/pkg/pipeline"
)

func main() {
    // Load prompts from embedded files
    prompts, _ := pipeline.LoadPrompts()

    // Create pipeline
    p, _ := pipeline.New(&pipeline.Config{
        LLM:           myLLMClient,      // implements pipeline.LLMClient
        Querier:       myQuerier,        // implements pipeline.Querier
        SchemaFetcher: mySchemaFetcher,  // implements pipeline.SchemaFetcher
        Prompts:       prompts,
        MaxRetries:    4,
    })

    // Run a query
    result, err := p.Run(ctx, "How many validators are connected to DZ?")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.Answer)
    fmt.Printf("Classification: %s\n", result.Classification)
    fmt.Printf("Data questions: %d\n", len(result.DataQuestions))
}
```

### With Conversation History

```go
history := []pipeline.ConversationMessage{
    {Role: "user", Content: "How many validators are connected?"},
    {Role: "assistant", Content: "There are 150 validators connected..."},
}

result, err := p.RunWithHistory(ctx, "What about their total stake?", history)
```

## Interfaces

### LLMClient

```go
type LLMClient interface {
    Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}
```

### Querier

```go
type Querier interface {
    Query(ctx context.Context, sql string) (QueryResult, error)
}
```

### SchemaFetcher

```go
type SchemaFetcher interface {
    FetchSchema(ctx context.Context) (string, error)
}
```

## File Structure

```
agent/
├── pkg/pipeline/
│   ├── pipeline.go      # Main orchestration, PipelineResult, Config
│   ├── classify.go      # Question classification (pre-step)
│   ├── decompose.go     # Question decomposition (step 1)
│   ├── generate.go      # SQL generation + retry logic (step 2)
│   ├── execute.go       # Query execution (step 3)
│   ├── synthesize.go    # Answer synthesis (step 4)
│   ├── respond.go       # Conversational responses
│   ├── schema.go        # Dynamic schema fetching
│   ├── anthropic.go     # Anthropic LLM client implementation
│   ├── querier.go       # Query result formatting
│   ├── prompts.go       # Prompt loading
│   └── prompts/
│       ├── CATALOG_SUMMARY.md   # Data catalog overview
│       ├── CLASSIFY.md          # Classification prompt
│       ├── DECOMPOSE.md         # Decomposition prompt
│       ├── GENERATE.md          # SQL generation prompt
│       ├── RESPOND.md           # Conversational response prompt
│       ├── SYNTHESIZE.md        # Answer synthesis prompt
│       └── embed.go             # Embeds prompts into binary
└── evals/
    ├── helpers_test.go                    # Test utilities
    ├── conversational_followup_test.go    # Conversational handling tests
    ├── unrelated_question_no_data_test.go # Out-of-scope tests
    ├── solana_validators_*.go             # Solana-related evals
    └── network_*.go                       # Network-related evals
```

## Evaluation Tests

The `evals/` directory contains end-to-end tests that validate pipeline behavior using real LLM calls. Tests support both Anthropic and local Ollama backends.

```bash
# Run with Anthropic
ANTHROPIC_API_KEY=... go test -tags evals ./evals/...

# Run with Ollama (local)
go test -tags evals ./evals/...

# Enable debug output
DEBUG=1 go test -tags evals -run TestName ./evals/...
```

## Design Decisions

### Why a Pipeline Instead of ReAct?

1. **Predictability**: Fixed steps mean consistent latency and cost
2. **Debuggability**: Each step's output is inspectable
3. **Parallelization**: Data questions execute concurrently
4. **Separation of concerns**: Each step has a single responsibility

### Why Dynamic Schema?

- Schemas evolve; embedding static schema would require redeployment
- Sample values help the LLM use correct enum values
- View definitions provide query hints

### Why Classification Pre-step?

- Avoids unnecessary database queries for conversational questions
- Provides natural handling of follow-ups and clarifications
- Graceful handling of out-of-scope questions

### Why Zero-Row Analysis?

- Empty results are often caused by incorrect filter values
- LLM can reason about whether zero rows is expected
- Automatic regeneration improves success rate without user intervention
