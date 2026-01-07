# Response Examples

Concrete patterns for correct and incorrect responses.

---

## Network Health Report

### ❌ Bad

```
Sure! Let me check the network health for you.

I found some issues in the network. There are devices that aren't activated and some links have problems. Here's what I discovered:

- 1 device with status: pending
- 1 link has packet loss
- Some interfaces have errors

Let me know if you need more details!
```

**Problems:**

- Starts with "Sure! Let me..."
- Missing device/link codes
- Missing percentages
- Missing time range
- No section headers
- Vague ("some interfaces")

### ✓ Good

```
### 🌐 Network Status (Past 24h)

**Devices**: 46 of 47 activated
- `chi-dzd1`: pending

**Links**: 83 of 84 activated
- `tok-fra-1`: soft-drained

**Packet Loss**:
- `nyc-lon-2`: 2.3% (A→B)

**Interface Errors**:
- `chi-dzd1` Ethernet4: 23 in_errors, 0 discards

**WAN Utilization**: All below 80%
```

---

## Latency Comparison

### ❌ Bad

```
The DZ network is faster than the public internet. Here are some numbers:

- NYC to London: DZ is 24ms, Internet is 68ms
- Tokyo to Singapore: DZ is about 35ms, Internet is around 85ms

DZ provides significant improvements across all routes.
```

**Problems:**

- No code block for dense metrics
- Inconsistent precision ("about", "around")
- Missing p95
- Wrong metro format
- Claims not from query results

### ✓ Good

````
### ⚡ DZ vs Internet (Past 24h)

```text
ROUTE           DZ AVG    DZ P95    INET AVG    INET P95    IMPROVEMENT
nyc → lon       24ms      26ms      68ms        74ms        65%
tok → sin       35ms      38ms      85ms        92ms        59%
chi → fra       52ms      55ms      98ms        108ms       47%
````

**Best**: `nyc → lon` (65% improvement)
**Lowest**: `chi → fra` (47% improvement)

```

---

## Validator Report

### ❌ Bad

```

5 validators disconnected from DZ in the last 24 hours. This represents a decrease in stake share.

```

**Problems:**
- Missing vote_pubkeys
- Missing IPs
- Missing stake amounts
- Missing timestamps
- No reconnection verification

### ✓ Good

```

### 🔌 Validator Disconnections (Past 24h)

2 validators disconnected and remain offline:

```text
VOTE_PUBKEY         IP              STAKE       TIME (UTC)
vote4abc...xyz      10.0.0.41       125,000 SOL 2024-01-15 14:23
vote7def...uvw      10.0.0.87       89,000 SOL  2024-01-15 18:45
```

**Total stake offline**: 214,000 SOL (0.3% of connected)

3 others disconnected but have since reconnected.

```

---

## User Traffic

### ❌ Bad

```

User abc123 used the most bandwidth yesterday with 500GB of traffic.

```

**Problems:**
- Uses unstable `pk`
- Missing `owner_pk` + `client_ip`
- Missing rate

### ✓ Good

```

### 📊 Top Bandwidth (Past 24h)

**`owner3abc...xyz` at `3.3.3.3` consumes the most bandwidth** with 847 GB (78 Mbps average).

```text
OWNER_PK            CLIENT_IP       TOTAL       AVG RATE
owner3abc...xyz     3.3.3.3         847 GB      78 Mbps
owner1def...uvw     1.1.1.1         523 GB      48 Mbps
owner9ghi...rst     9.9.9.9         412 GB      38 Mbps
```

Location determined via GeoIP.

```

---

## Missing Data

### ❌ Bad

```

The latency for tok-fra-1 appears to be around 45ms based on typical routes.

```

**Problem:** Invents data.

### ✓ Good

```

### ⚠️ Data Unavailable

No latency samples for `tok-fra-1` in the past 24 hours.

Possible causes:

- Recently provisioned
- Currently drained
- Telemetry collection issue

Check status: `SELECT * FROM dz_links_current WHERE code = 'tok-fra-1'`

```

---

## Incident Timeline

### ❌ Bad

```

The link had some issues yesterday. There was packet loss and then it was drained.

```

**Problems:**
- No timestamps
- No elapsed time
- Vague ("some issues", "yesterday")
- No device/interface specifics

### ✓ Good

```

### 🕐 tok-fra-1 Incident Timeline

```text
TIME (UTC)              EVENT                           DETAILS
2024-01-15 08:00        Baseline                        0% loss, 0 errors
2024-01-15 10:23        Packet loss spike               12% loss (A→B)
2024-01-15 10:24        Interface errors                chi-dzd1 Eth1: 47 in_errors
2024-01-15 10:31        Soft drain applied              isis_delay_override set
2024-01-15 14:45        Drain removed                   isis_delay_override cleared
2024-01-15 14:46        Recovery                        0% loss, 0 errors
```

**Duration**: 4h 23m (10:23 → 14:46)
**Impact**: 12% peak packet loss on A→B direction

```

---

## Pattern Summary

| Query Type | Must Include |
|------------|--------------|
| Network health | Device codes, link codes, percentages, time range |
| Latency comparison | Code block, avg + p95, improvement % |
| Validators | vote_pubkey, IP, stake, timestamp |
| Users | owner_pk, client_ip, total + rate |
| Incidents | Timestamps, elapsed time, device + interface |
| Missing data | Explicit statement, no guessing |
```
