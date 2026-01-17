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
- **Currently in view**: Yes (`status_change` event type)

### 2. ISIS Delay Override (Effective Soft-Drain)
- Link `isis_delay_override_ns` set to 1s (1,000,000,000 ns)
- This makes the link less preferred in routing, effectively soft-draining it
- Does NOT change the `status` field - so it's distinct from status changes
- Source: `dim_dz_links_history.isis_delay_override_ns`
- Timestamp precision: Exact
- **Currently in view**: NO - gap to fill

### 3. Packet Loss
- Link experiencing measurable packet loss
- Source: `fact_dz_device_link_latency.loss`
- Timestamp precision: Hourly (aggregated)
- **Currently in view**: Yes (`packet_loss` event type, threshold >= 0.1%)

**Severity levels** (proposed):
| Severity | Loss % | Notes |
|----------|--------|-------|
| Minor | < 1% | Detectable but likely not impactful |
| Moderate | 1% - 10% | Noticeable degradation |
| Severe | >= 10% | Significant impact |

**Open question**: Should severity be based on:
- Peak loss % in a single hour?
- Average loss % over the event duration?
- Some rolling window calculation?

### 4. Latency SLA Breach
- Link measured RTT exceeds committed RTT significantly
- Source: `fact_dz_device_link_latency.rtt_us` vs `dim_dz_links.committed_rtt_ns`
- Timestamp precision: Hourly (aggregated)
- **Currently in view**: Yes (`sla_breach` event type, threshold >= 20% over committed)

**Open question**: Should there be severity levels for SLA breaches too?

### 5. Missing Telemetry (Link Dark)
- No latency samples received for extended period
- Could indicate: link down, monitoring failure, or connectivity issue
- Source: gaps in `fact_dz_device_link_latency`
- Timestamp precision: Hourly
- **Currently in view**: Yes (`missing_telemetry` event type, threshold >= 120 minutes)

## View Implementation (`dz_link_issue_events`)

View location: `migrations/0005_link_issue_events_view.sql`

| event_type | Trigger | Key Columns |
|------------|---------|-------------|
| `status_change` | Status changed FROM activated | `previous_status`, `new_status` |
| `isis_delay_override_soft_drain` | isis_delay_override_ns set to 1s | - |
| `packet_loss` | Loss >= 0.1% | `loss_pct` |
| `missing_telemetry` | Telemetry gap >= 120 min | `gap_minutes` |
| `sla_breach` | Latency >= 20% over committed | `overage_pct`, `latency_us` |

## Design Decisions

1. **View stays simple** - surfaces all issues with raw metrics, no severity classification
2. **Severity is query-time** - queries/agent apply thresholds based on context
3. **"Outage" is presentation-layer** - agent decides what constitutes an outage based on:
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

## Implementation Status

All items completed:
- [x] Added ISIS delay override detection → `isis_delay_override_soft_drain`
- [x] Renamed `link_dark` → `missing_telemetry`
- [x] Renamed view → `dz_link_issue_events`
- [x] Updated agent prompts (DECOMPOSE.md, GENERATE.md, SYNTHESIZE.md) with new names and severity guidelines

## Next Steps

1. Align status page UI with these definitions (see `docs/plans/status-page-link-issues-alignment.md`)
