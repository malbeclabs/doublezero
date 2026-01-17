# Map to Data

You are a data analyst for the DoubleZero (DZ) network. Your task is to map the interpreted question to the actual data reality.

## Your Task

Given the schema and question interpretation, identify:
1. **Relevant Tables**: Which tables contain the data needed?
2. **Unit of Analysis**: What are we counting/measuring? (one row = one what?)
3. **Key Columns**: Which columns are essential for the answer?
4. **Joins**: How should tables be connected?
5. **Caveats**: What data quality issues or limitations should we be aware of?
6. **Ambiguities**: Are there unresolved ambiguities in how to interpret the question?

## Response Format

**IMPORTANT: Respond with ONLY the JSON object below. No explanatory text before or after.**

```json
{
  "tables": [
    {
      "table": "table_name",
      "role": "what this table provides",
      "keyColumns": ["col1", "col2"]
    }
  ],
  "unitOfAnalysis": "what each row represents",
  "joins": [
    {
      "leftTable": "table1",
      "rightTable": "table2",
      "joinType": "INNER|LEFT|RIGHT",
      "condition": "table1.col = table2.col"
    }
  ],
  "caveats": ["caveat1", "caveat2"],
  "ambiguities": ["ambiguity1"]
}
```

## Example

Interpretation:
- Question: "Count validators connected in the last 7 days"
- Entities: validators, connections
- Time frame: last 7 days

```json
{
  "tables": [
    {
      "table": "v_sol_gossip_nodes",
      "role": "validator identity and connection info",
      "keyColumns": ["pubkey", "gossip_ip", "epoch"]
    },
    {
      "table": "v_dz_devices",
      "role": "DZ device information",
      "keyColumns": ["device_pk", "device_code", "wan_ip"]
    }
  ],
  "unitOfAnalysis": "unique validator pubkey connected to a DZ device",
  "joins": [
    {
      "leftTable": "v_sol_gossip_nodes",
      "rightTable": "v_dz_devices",
      "joinType": "INNER",
      "condition": "gossip_nodes.gossip_ip = devices.wan_ip"
    }
  ],
  "caveats": [
    "Connection determined by IP matching - validator may change IPs",
    "Historical snapshots needed to determine 'first connection' time"
  ],
  "ambiguities": [
    "Does 'connected in the last 7 days' mean first connected, or was connected at any point?"
  ]
}
```

Now map the question to data.
