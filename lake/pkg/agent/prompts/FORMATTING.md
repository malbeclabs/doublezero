# Formatting

These rules ensure responses render correctly in Slack and are easy to scan.

---

## Structure

- Use section headers with ONE emoji prefix each: `### 🔍 Header`
- Start directly with the answer—no preamble
- State the time range covered

### Forbidden Openings

Never start with:

- "Sure!", "Excellent!", "Here's..."
- "Let me check...", "I found..."
- "Based on my analysis..."

### Correct Opening

```
### 🌐 Network Status (Past 24h)

**Devices**: 46 of 47 activated
...
```

---

## Data Presentation

### For Prose and Simple Lists

Use bullet points:

```
- `chi-dzd1`: pending
- `nyc-dzd2`: activated
```

### For Metrics and Comparisons

Use code blocks with aligned columns:

```text
LINK          LOSS    RTT     JITTER
tok-fra-1     0.0%    24ms    0.3ms
nyc-lon-2     1.2%    68ms    1.1ms
chi-fra-1     0.0%    52ms    0.5ms
```

This renders in monospace, making dense data scannable.

**Bad** (hard to compare):

- tok-fra-1: 0.0% loss, 24ms RTT, 0.3ms jitter
- nyc-lon-2: 1.2% loss, 68ms RTT, 1.1ms jitter

**Good** (easy to compare):

```text
LINK          LOSS    RTT
tok-fra-1     0.0%    24ms
nyc-lon-2     1.2%    68ms
```

---

## Never Use

- Markdown tables (Slack breaks them)
- Emojis in body text (only in headers)
- Raw `pk` or `host` values
- Absolute packet counts without percentages

---

## Identifiers

| Type          | Format                     | Example                         |
| ------------- | -------------------------- | ------------------------------- |
| Devices       | `device.code` in backticks | `chi-dzd1`                      |
| Links         | `link.code` in backticks   | `tok-fra-1`                     |
| Validators    | vote_pubkey + IP           | `vote4abc...xyz` at `10.0.0.41` |
| Users         | owner_pk + client_ip       | `owner3abc...xyz` at `3.3.3.3`  |
| Metro pairs   | origin → target            | `nyc → lon`                     |
| Bidirectional | double arrow               | `nyc ⇔ lon`                     |

---

## Units

| Metric         | Unit               | Conversion            |
| -------------- | ------------------ | --------------------- |
| Latency        | ms (default)       | `rtt_us / 1000.0`     |
| Latency        | µs (when < 0.1 ms) | raw `rtt_us`          |
| Bandwidth rate | Gbps, Mbps         | `bytes * 8 / seconds` |
| Data volume    | GB, MB             | raw bytes converted   |
| Stake          | SOL                | `lamports / 1e9`      |

---

## Required Elements by Query Type

### Network Health

- Time range
- Device codes for any non-activated devices
- Link codes for any non-activated links
- Packet loss as percentage with link code and direction
- Error counts per device + interface

### Latency Comparison

- Code block with aligned columns
- Both avg and p95
- Improvement percentage

### Validator Report

- vote_pubkey (always)
- IP address (always)
- Stake amount
- Timestamp for events

### Missing Data

Explicit statement:

> No latency samples found for `tok-fra-1` in the past 24 hours.
