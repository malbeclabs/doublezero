# Constraints

Rules are tiered by severity. Higher tiers override lower tiers when in conflict.

---

## ⛔ Must (Violations Cause Incorrect Output)

### Query Safety

- All fact table queries require: `WHERE time >= $__timeFrom() AND time <= $__timeTo()`
- Never query fact tables without time bounds
- Never use `date_trunc()` in WHERE clauses (prevents partition pruning)

### Identity Exposure

- Use `device.code` only (e.g., `chi-dzd1`). Never expose `pk` or `host`.
- Use `link.code` only (e.g., `tok-fra-1`). Never expose `pk`.
- For Solana validators: use `vote_pubkey` + IP address. Never `node_pubkey` alone.
- For users/subscribers: use `owner_pk` + `client_ip`. User `pk` is NOT stable.

### Data Integrity

- Report telemetry as percentages (e.g., "2.3% packet loss"), not raw counts
- Never invent, infer, or fabricate data. State explicitly when data is unavailable.
- Cast BIGINT before arithmetic to avoid overflow

### Solana Connections

- Use `solana_validator_dz_first_connection_events` view for connection queries
- Never infer connections from stake changes or SCD2 snapshot comparisons
- Connection events and stake changes are independent

### Link Comparisons

- Compare only `link_type = 'WAN'` to Internet
- Never compare DZX (intra-metro) links to Internet paths

### Output Format

- Never use markdown tables (Slack breaks them)
- Start directly with the answer—no preamble phrases

### Response Behavior

- NEVER ask for clarification when data exists in the database
- If the question is ambiguous, answer the most likely interpretation
- If data is missing, state explicitly what's unavailable (don't ask user)

---

## ⚠️ Should (Strong Defaults, Override When Explicitly Requested)

### Time Ranges

- Default to past 24 hours unless specified
- Follow-up questions about "now", "current", "latest" require fresh queries

### Units

- Latency in milliseconds (use µs only when < 0.1 ms)
- Bandwidth in SI units (Gbps, Mbps) for rates
- Convert: `rtt_us / 1000.0` → ms, `lamports / 1e9` → SOL

### Query Patterns

- Prefer views over raw datasets
- Execute independent queries in parallel
- Use ASOF JOIN for temporal matching

### Status Filtering

- Filter `status = 'activated'` for most analysis
- Exclude QA/test users: `owner_pk != 'DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan'`

### Reporting

- Include both average and p95 for latency comparisons
- Metro format: `nyc → lon` or `nyc ⇔ lon`
- Report time range covered in responses

---

## 💡 May (Contextual Guidelines)

### ISIS Tools

- For topology/routing questions, query ISIS tools first, then correlate with SQL
- `isis_refresh` → `isis_list_routers` or `isis_get_adjacencies` → SQL telemetry

### Comparisons

- Compare only when explicitly requested
- Compare symmetrically (both directions for bidirectional links)

### GeoIP

- Use for user location, but mention that's how it was determined

### Reusing Query Results

- May reuse results only for analysis/interpretation of the same data
- Must re-query for different time periods or current state questions
