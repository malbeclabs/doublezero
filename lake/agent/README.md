# Lake Analysis Agent

An LLM-powered agent for answering natural language questions about DoubleZero network and Solana validator data.

## Overview

The agent transforms natural language questions into database queries, executes them, and synthesizes the results into comprehensive answers. It uses a tool-calling workflow where the LLM iteratively reasons about the question and executes queries until it has enough data to answer.

**Data sources:**
- **ClickHouse** (SQL): Network telemetry, metrics, time-series data, and Solana validator statistics
- **Neo4j** (Cypher): Network topology, device relationships, path finding, and connectivity analysis

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
│                    │                │                │                      │
│                    ▼                ▼                ▼                      │
│             ┌────────────┐   ┌─────────────┐   ┌───────────┐               │
│             │execute_sql │   │execute_cypher│   │ read_docs │               │
│             │            │   │             │   │           │               │
│             │ClickHouse  │   │   Neo4j     │   │   Docs    │               │
│             │  queries   │   │  queries    │   │  lookup   │               │
│             └────────────┘   └─────────────┘   └───────────┘               │
│                    │                │                │                      │
│                    └────────────────┴────────────────┘                      │
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
│  • Tables for multi-attribute data                                          │
│  • Caveats and limitations                                                  │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Tools

The agent has access to three tools:

| Tool | Database | Purpose |
|------|----------|---------|
| `execute_sql` | ClickHouse | Time-series data, metrics, aggregations, validator stats, historical analysis |
| `execute_cypher` | Neo4j | Topology, path finding, reachability, connectivity, impact analysis |
| `read_docs` | - | DoubleZero documentation for conceptual and setup questions |

### When to Use Each Tool

**execute_sql** - Listing, metrics, status:
- "Show all devices" / "List links" / "What metros exist"
- "What's the average latency?" / "Show bandwidth utilization"
- "How many validators are on DZ?" / "Show validator stakes"
- Historical trends and aggregations

**execute_cypher** - Paths, reachability, impact:
- "What's the path from NYC to LON?"
- "What devices are reachable from Tokyo?"
- "What's affected if chi-dzd1 goes down?"
- Multi-hop connectivity analysis

**read_docs** - Conceptual and procedural:
- "What is DoubleZero?"
- "How do I connect to DZ?"
- "Why isn't my tunnel working?"

## Workflow

The agent follows an iterative workflow:

### 1. Understand the Question
- What type of question? (descriptive, comparative, diagnostic)
- What entities and time windows are implied?
- Which data source is appropriate? (ClickHouse, Neo4j, or both)

### 2. Execute Queries
- Call `execute_sql` and/or `execute_cypher` with actual queries
- Batch independent queries for parallel execution
- Query for specific entity identifiers, not just aggregates

### 3. Iterate if Needed
- Adjust filters after seeing real distributions
- Query for specific identifiers if only aggregates returned
- Investigate if results contradict expectations

### 4. Synthesize
- State what the data shows, not what it implies
- Tie each claim to an observed metric with [Q1], [Q2] references
- Use tables for multi-attribute lists

## Question Types

| Type | Handling |
|------|----------|
| **Data Analysis** | Questions requiring SQL/Cypher queries - uses full workflow |
| **Documentation** | Conceptual questions about DZ - uses `read_docs` tool |
| **Conversational** | Clarifications, follow-ups - direct response without queries |
| **Out of Scope** | Unrelated questions - polite redirect |

## Claim Attribution

Every factual claim references its source query (e.g., `[Q1]`):
- Users can trace any claim back to the data
- Builds trust through transparency
- Makes it easy to verify specific numbers

Example:
> There are 150 validators on DZ [Q1], with total stake of ~12M SOL [Q2].

## Running Evals

The agent has an evaluation suite to verify behavior:

```bash
./scripts/run-evals.sh                 # Run all evals
./scripts/run-evals.sh -f 'TestName'   # Run specific test
./scripts/run-evals.sh -s              # Short mode (no API calls)
./scripts/run-evals.sh --show-failures # Show failure logs at end
```

Output goes to `eval-runs/<timestamp>/` with individual test logs and summary files.

## Design Decisions

### Why Tool-Calling?

1. **Flexibility**: The agent can execute as many queries as needed
2. **Iteration**: Results inform the next step, allowing refinement
3. **Transparency**: Query history is visible to users
4. **Natural flow**: Mirrors how a human analyst would work

### Why Two Databases?

- **ClickHouse**: Optimized for time-series analytics and aggregations
- **Neo4j**: Optimized for graph traversal and relationship queries
- Some questions benefit from both (e.g., "latency on the path from NYC to LON")

### Why Dynamic Schema?

- Schemas evolve; static schema would require redeployment
- Sample values help the LLM use correct enum values
- View definitions provide query hints
