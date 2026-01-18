# Plan: Align Status Page with Link Issue Definitions

## Context

We've defined a clear domain model for link issues in `docs/domain/link-issues.md`. The status page and timeline currently use similar but not identical concepts. This plan aligns them for consistency across the agent, status page, and timeline.

## Current State

### Status Page Issue Types
| Current Name | Trigger | Notes |
|--------------|---------|-------|
| `packet_loss` | Loss >= 0.1% | Matches domain |
| `high_latency` | Latency > 20% over committed RTT | Same as `sla_breach` in domain |
| `disabled` | Status drained OR 95%+ loss for 1h | Conflates two concepts |

### Status Page Health Categories
| Category | Current Definition |
|----------|-------------------|
| Healthy | Loss < 0.1%, latency within 20% of committed |
| Degraded | Loss >= 0.1%, latency 20-50% over committed |
| Unhealthy | Loss >= 1%, latency > 50% over committed |
| Disabled | Extended packet loss (>=95% over 1h) OR drained status |

### Domain Link Health Views
Two views provide link health information:

**`dz_links_health_current`** (current state with boolean flags):
| Column | Description |
|--------|-------------|
| `is_soft_drained` | Status is 'soft-drained' |
| `is_hard_drained` | Status is 'hard-drained' |
| `is_isis_soft_drained` | ISIS delay override set to 1000ms |
| `has_packet_loss` | Loss >= 1% in last hour |
| `exceeds_committed_rtt` | Avg latency exceeds committed RTT |
| `is_dark` | No telemetry in last 2 hours |

**`dz_link_status_changes`** (historical status transitions):
| Column | Description |
|--------|-------------|
| `previous_status`, `new_status` | Status transition |
| `changed_ts` | When the change occurred |

## Gaps

1. **"Disabled" conflates two concepts**: Currently "disabled" means either (a) status is drained, or (b) extreme packet loss. These should be distinct:
   - Status-based: `status_change` or `isis_delay_override_soft_drain`
   - Telemetry-based: `missing_telemetry` (no data) vs severe `packet_loss`

2. **Missing ISIS delay override**: The status page doesn't detect ISIS delay override as a form of soft-drain.

3. **No "missing telemetry" in UI**: The status page doesn't show `missing_telemetry` events.

4. **Terminology mismatch**: UI uses "high_latency" but domain uses "sla_breach".

5. **Severity levels not surfaced**: Domain defines packet loss severity (minor/moderate/severe) but UI doesn't use this.

## Proposed Changes

### Phase 1: Rename and Clarify (Terminology Alignment)

1. **Rename in UI**: `high_latency` → `sla_breach` (match domain)

2. **Split "disabled" into distinct concepts**:
   - `drained` - for status_change events (soft-drained, hard-drained)
   - `isis_soft_drain` - for isis_delay_override_soft_drain events
   - `missing_telemetry` - for telemetry gaps
   - Remove `disabled` as a category (it's a derived state, not a root cause)

3. **Update issue filter chips**:
   - Current: Loss | Latency | Disabled
   - Proposed: Loss | SLA Breach | Drained | No Data

### Phase 2: API Alignment

Options for the status page API:

**Option A: Use the views directly**
- Query `dz_links_health_current` and `dz_link_status_changes` from the status API
- Pros: Single source of truth, consistency
- Cons: May not suit all real-time status needs

**Option B: Parallel logic, same definitions**
- Keep existing status cache queries but align thresholds/definitions
- Pros: Optimized for real-time status (short lookback, cached)
- Cons: Two implementations to maintain

**Recommendation**: Option B for now. The status page needs:
- Short lookback (1h-7d) with fine granularity
- Cached/fast responses
- Real-time updates

The view is better for historical analysis (agent queries, incident investigation).

### Phase 3: Add Missing Issue Types

1. **Add ISIS delay override detection** to status cache:
   - Check `isis_delay_override_ns = 1000000000` in link history
   - Surface as "Effective Soft-Drain" or similar

2. **Add missing telemetry detection** to status cache:
   - Already partially exists (the "no data" bucket in timeline)
   - Surface as a distinct issue type with badge

### Phase 4: Severity in UI

1. **Packet loss severity colors/badges**:
   - Minor (< 1%): Yellow badge
   - Moderate (1-10%): Orange badge
   - Severe (>= 10%): Red badge

2. **SLA breach severity** (optional):
   - Minor (20-50% over): Yellow
   - Severe (> 50% over): Red

## Implementation Tasks

### API Changes (`api/handlers/status.go`)
- [ ] Add ISIS delay override detection to link health calculation
- [ ] Rename `high_latency` to `sla_breach` in response
- [ ] Split `disabled` into `drained` + `missing_telemetry`
- [ ] Add severity field for packet_loss events

### Frontend Changes (`web/src/components/status-page.tsx`)
- [ ] Update `IssueFilter` type and labels
- [ ] Update `IssueCounts` interface
- [ ] Update filter chips and badge colors
- [ ] Add severity-based coloring for packet loss

### Frontend Changes (`web/src/components/link-status-timelines.tsx`)
- [ ] Update issue_reasons handling for new types
- [ ] Update badges for new terminology
- [ ] Add severity indicators

### Timeline Changes (`web/src/components/status-timeline.tsx`)
- [ ] Ensure "no data" buckets are distinct from "disabled"
- [ ] Add tooltip details for each bucket type

## Migration Notes

- API changes are additive first (keep old fields, add new ones)
- Frontend can switch once API is ready
- No database migration needed (uses existing data, just interprets differently)

## Open Questions

1. Should we show ISIS delay override as a separate badge, or group with "drained"?
2. For the timeline, should "missing_telemetry" be a different color than current "no data"?
3. Do we need backward compatibility for external consumers of the status API?
