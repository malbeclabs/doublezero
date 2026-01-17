# Link Issues - Domain Definition

This document defines what constitutes a link "issue" in the DZ network.

**Terminology**:
- **Issue** = any degradation or problem with a link (broad term)
- **Outage** = sustained/severe issues that significantly impact service (narrower term, TBD based on duration + severity)

## Issue Types

### 1. Status Change
- Link status transitions from `activated` to `soft-drained` or `hard-drained`
- Source: `dim_dz_links_history.status`
- Timestamp precision: Exact
- **View**: `dz_link_status_changes` (historical), `dz_links_health_current.is_status_degraded` (current)

### 2. ISIS Delay Override (Effective Soft-Drain)
- Link `isis_delay_override_ns` set to 1s (1,000,000,000 ns)
- This makes the link less preferred in routing, effectively soft-draining it
- Does NOT change the `status` field - so it's distinct from status changes
- Source: `dim_dz_links_history.isis_delay_override_ns`
- Timestamp precision: Exact
- **View**: `dz_links_health_current.is_isis_soft_drained` (current)

### 3. Packet Loss
- Link experiencing measurable packet loss
- Source: `fact_dz_device_link_latency.loss`
- Timestamp precision: Hourly (aggregated)
- **View**: `dz_links_health_current.has_packet_loss`, `dz_links_health_current.loss_pct` (current)

**Severity levels** (proposed):
| Severity | Loss % | Notes |
|----------|--------|-------|
| Minor | < 1% | Detectable but likely not impactful |
| Moderate | 1% - 10% | Noticeable degradation |
| Severe | >= 10% | Significant impact |

### 4. Latency SLA Breach
- Link measured RTT exceeds committed RTT significantly
- Source: `fact_dz_device_link_latency.rtt_us` vs `dim_dz_links.committed_rtt_ns`
- Timestamp precision: Hourly (aggregated)
- **View**: `dz_links_health_current.exceeds_committed_rtt` (current)

### 5. Missing Telemetry (Link Dark)
- No latency samples received for extended period
- Could indicate: link down, monitoring failure, or connectivity issue
- Source: gaps in `fact_dz_device_link_latency`
- Timestamp precision: Hourly
- **View**: `dz_links_health_current.is_dark` (current, 2-hour threshold)

## View Implementation

Two views provide link health information:

### `dz_links_health_current` (Current State)
Shows current health state of each link with boolean flags.

| Column | Description |
|--------|-------------|
| `is_status_degraded` | Status is not 'activated' |
| `is_isis_soft_drained` | ISIS delay override set to 1s |
| `has_packet_loss` | Loss >= 1% in last hour |
| `loss_pct` | Packet loss percentage (last hour) |
| `exceeds_committed_rtt` | Avg latency exceeds committed RTT |
| `avg_rtt_us`, `p95_rtt_us` | Latency metrics (last hour) |
| `is_dark` | No telemetry in last 2 hours |

### `dz_link_status_changes` (Historical)
Shows all status transitions for links.

| Column | Description |
|--------|-------------|
| `link_pk`, `link_code` | Link identifier |
| `previous_status`, `new_status` | Status transition |
| `changed_ts` | When the change occurred |
| `side_a_metro`, `side_z_metro` | Metro codes |

### For Packet Loss / Latency History
Query the raw `fact_dz_device_link_latency` table directly with time filters.

## Design Decisions

1. **Two focused views** - One for current state (boolean flags), one for historical changes
2. **Severity is query-time** - Queries/agent apply thresholds based on context
3. **"Outage" is presentation-layer** - Agent decides what constitutes an outage based on:
   - Issue type
   - Severity (from raw metrics)
   - Duration
   - Link criticality (if relevant)

## Severity Guidelines (for agent prompts)

**Packet Loss**:
| Severity | Loss % |
|----------|--------|
| Minor | < 1% |
| Moderate | 1% - 10% |
| Severe | >= 10% |

**SLA Breach**: TBD - may want similar tiering based on overage %

## Next Steps

1. Align status page UI with these definitions (see `docs/plans/status-page-link-issues-alignment.md`)
