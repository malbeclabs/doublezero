# Eval Failures to Investigate

Logged from eval run on 2026-01-16.

## 1. TestLake_Agent_Evals_Anthropic_NetworkStateSummary

**Status:** Flaky infrastructure failure

**Error:**
```
failed to create shared DB: failed to get ClickHouse container mapped port: port "9000/tcp" not found
```

**Analysis:**
This is a Docker/testcontainers infrastructure issue, not a code problem. The ClickHouse container started but the native port (9000) wasn't mapped correctly. This is likely a race condition or resource contention issue.

**Potential fixes:**
- Add retry logic for container port mapping
- Increase container startup timeout
- Check if there are orphaned containers from previous runs

---

## 2. TestLake_Agent_Evals_Anthropic_SolanaValidatorsOnDZVsOffDZ

**Status:** Evaluator model incorrectly assessed the response

**Agent Response (correct):**
```
📊 **Solana Validator Comparison: On-DZ vs Off-DZ**

**Population Overview** [Q1]
- **On DZ**: 3 validators with 3,700 SOL total stake
- **Off DZ**: 3 validators with 5,400 SOL total stake

**Performance Comparison** [Q1]

| Metric | On DZ | Off DZ |
|--------|-------|--------|
| **Avg Stake per Validator** | 1,233 SOL | 1,800 SOL |
| **Avg Vote Lag** | 50 slots | 200 slots |
| **Avg Skip Rate** | 2.0% | 8.0% |

**Key Findings** [Q1]

On-DZ validators show significantly better performance:
- **Vote lag is 4x lower** (50 vs 200 slots)
- **Skip rate is 4x lower** (2.0% vs 8.0%)
```

**Evaluator Reasoning (questionable):**
```
The agent's response fails to meet the critical expectations because there is no
verifiable evidence that the data presented is authentic or sourced from actual
Solana validator information. The response provides plausible-sounding metrics
but:
1. Does not reference actual validator identities (vote1, vote2, etc.)
2. Does not cite a data source or timestamp
3. Appears to be fabricated data rather than actual Solana on-chain statistics
```

**Analysis:**
The agent correctly queried the test database and returned accurate data. The evaluator (haiku) incorrectly assessed the response as "fabricated" because it expected the agent to include raw validator identities in a summary comparison. This is an eval expectation/evaluator prompt issue, not an agent issue.

**Potential fixes:**
- Update the eval expectations to be clearer about what constitutes a valid response
- Modify the evaluator prompt to understand that test data is valid data
- Have the agent include validator identities in the response (but this may clutter summary views)
- Consider if the question/expectations mismatch - "compare on-DZ vs off-DZ" is a summary question, not a detail question
