# RFC - Network contributor incident and maintenance logging

# Summary

This RFC proposes a gateway-first mechanism for creating and updating incidents and planned maintenance across the DoubleZero network. Contributors use a single, versioned API to publish operational events while retaining their internal tools. The gateway validates identity (API key bound to contributor), enforces enumerations, normalizes payloads, stores them in a  datastore, and notifies coordination channels in Slack.

Contributors can create incidents or maintenance via a shared form (backed by Airtable or similar) which submits through the API gateway, or directly through the API itself. Slack remains the primary interface for awareness, conversation, and history, all open and historical events appear in `#contributor-incidents` and `#contributor-maintenance`.

Over time, minimal facts are anchored on the DoubleZero ledger so any party can verify contributor-reported status or build independent explorers without relying on centralized databases.

# **Motivation**

Operational awareness across contributors is fragmented in private systems and Slack DMs / Threads. We need a uniform, verifiable way to publish incidents (unplanned) and maintenance (planned), with searchable shared context and cross-contributor visibility.

A unified gateway gives one authoritative ingress for operational events. Slack remains the collaboration surface, but the canonical record of status and lifecycle changes becomes machine-verifiable and future-proof for decentralization.

This design delivers immediate value (shared visibility, consistent status logs, network-wide coordination) while maintaining a clean migration path to on-chain anchoring.

# **New Terminology**

| Term | Definition |
| --- | --- |
| **Incident** | Unplanned service-impacting event with enumerated severity and status. |
| **Maintenance** | Planned, time-bounded activity that may affect availability. Auto-closes after its window unless updated. |
| **Proof of Contributor** | Caller authority via per-contributor API key (MVP); later, signed requests and on-chain registry. |
| **Gateway** | Authenticated, versioned ingress that validates identity, enforces schema, persists events, and emits notifications. |

# **Alternatives Considered**

- **Do nothing:** Keeps fragmented visibility and manual coordination.
- **Point-to-point bridges:** N² maintenance trap across diverse contributor systems.
- **Mandate one product:** Simplifies normalization but undermines autonomy.
- **Go fully on-chain day one:** Slows adoption and tooling readiness.

A **gateway-first** design achieves fast adoption and a clear path to progressive decentralization.

# **Detailed Design**

## Architecture

- **Ingress**: Versioned REST API and web form (writes through the API).
- **Identity**: API key bound to a Contributor `pubkey` (service key).
- **Storage**: Airtable (MVP) →  on-chain anchors.
- **Notifications**: Slack channel for new records, status changes (MVP).

## Cross-Contributor Tickets

- Any contributor can open a ticket for any device or link. (e.g., observed issues on another contributors device/link).
- Contributors can mention each other by using the slack `@<conbributor>`.

## Maintenance Events

- Fields include planned `start_at`, `end_at`, affected devices/links, and free-form notes.
- **Auto-close**: Maintenance transitions to `closed` 24 hours after `end_at`.
- Visible to all contributors to avoid overlapping maintenance windows.

## Visibility and Replies

- All contributors can view all open tickets (incidents + maintenance) and append updates/comments in slack threads.

## Commenting, Updates, and Attachments (Slack, API, On-Chain)

**Design choice (MVP)**: Single ingress for structured facts (open ticket / create maintenance, close ticket / close maintenance) via API/webform; Slack used for human conversation and notifications.

**Why**:

- **Consistency**: Slack threads differ in structure, permissions, edits/deletes, and attachments. Bi-directional real-time sync introduces race conditions and partial failures.
- **Clarity**: One authoritative store simplifies future on-chain anchoring and auditability.
- **Pragmatism**: Teams can still converse in Slack. Which makes it easy to share logs, screenshots or start a call to collaborate on a particular issue.

**How (MVP)**:

- The gateway forces the contributor to fill in all required fields and authenticates via an API key that is bound to the contributor. This makes sure we capture all required information which we could also use to start developing the on-chain registration of incidents/maintenance.
- The gateway posts a Slack message per ticket and maintenance item in `#contributor-incidents` or `#contributor-maintenance`

## Identity Stages

- **Now (MVP)**: API key per contributor bound to `pubkey`.
- **Later**: On-chain registry binds `pubkey` to contributor.

## Enumerations

| Field | Options | Notes |
| --- | --- | --- |
| `type` | `incident`, `maintenance` | Record category. |
| `status` | `open`, `closed` | We can extend this later, see Future expansions below. |
| `severity` | `sev1`, `sev2`, `sev3` | Required for incidents; optional/default `sev3` for maintenance. |

## Canonical Schema (v1) — **Tables and Fields**

### A. **Ticket** (Incident or Maintenance)

| Field | Description | Required | Notes | Later stored on chain |
| --- | --- | --- | --- | --- |
| `id` | System-assigned unique identifier | Auto | UUID/hash | no |
| `type` | `incident` or `maintenance` | Yes | Drives required fields | yes |
| `title` | Short summary | Yes |  | no |
| `description` | Detailed explanation | Yes |  | no |
| `pubkey` | Creator contributor public key | Yes | Binds to API key | yes |
| `reporter_name` | Human name (optional) | Optional | Captured from Portal/API | no |
| `reporter_email` | Human email (optional) | Optional |  | no |
| `device_pubkey` | Devices implicated | yes* | list of pubkeys affected | yes |
| `affected_link_pubkey` | Links/assets implicated | yes* | list of pubkeys affected | yes |
| `severity` | Enumerated | Incidents | Optional/default for maintenance | no |
| `status` | Enumerated | Yes |  | yes |
| `start_at` | Start timestamp (UTC) | Incidents: Optional (first observed). Maintenance: Planned start |  | yes |
| `end_at` | End timestamp (UTC) | Optional | Maintenance: planned end | yes |
| `auto_close_after_hours` | Hours after `end_at` to auto-close if no updates | Default 24 hours for maintenance |  | no |
| `created_at` / `updated_at` | Timestamps | Auto |  | Yes |

**At least one* `device_pubkey` *or one* `affected_link_pubkey` *is required to be filled in*

### B. **Update**

| Field | Description | Required | Later stored on chain |
| --- | --- | --- | --- |
| `update_id` | Unique id | Auto | no |
| `ticket_id` | Parent ticket | Yes | no |
| `author_pubkey` | Contributor author (if any) | Yes | yes |
| `timestamp` | Event time | Auto | yes |
| `body` | Comment text or summary | Optional | no |
| `status_from` / `status_to` | For status changes | Conditional | yes |

## Notifications

- **Create / Close incident**: Post to `#contributor-incidents` with ticket id, status, severity, device and link pubkeys should be resolved to human readable device and linknames, name of the contributor who created / closed the incident.
- **Create / Close maintenance**: Post to `#contributor-maintenance` with ticket id, status, device and link pubkeys should be resolved to human readable device and linknames, planned start time and planned end time, name of the contributor who created / closed the maintenance.
- **Maintenance awareness**: Post extra maintenance reminder 24 hours prior to the start of a maintenance action.

## Storage and Lifecycle

- **Airtable (MVP)**: Easy way to store highlevel data of incidents and maintenance.
- **Maintenance auto-close**: Close 24 hours after `end_at`.
- **Incidents**: Manual close, via form or direct via the API.
- Slack: should follow our internal data retention policy.

## Going Fully On-Chain: Trade-offs

- **Pros**: Public verifiability; replayable audit; independent explorers; resilience.
- **Cons**: Higher engineering lift now; comments/attachments require off-chain CAS; Rich media UX still need portals.

**Path**: Use the API gateway + Slack now for fast adoption. In parallel, build the on-chain registration of incidents/maintenance. 

# **Impact**

- Immediate, shared visibility for incidents and maintenance.
- Cross-contributor visibility and possibility to collaborate.
- Dedicated Slack channels for contributor incidents and contributor maintenance.
- Slack remains useful for collaboration, sharing rich media and visibility.
- Clear migration path to verifiable, on-chain anchors.

# **Security Considerations**

- Authentication via API key bound to contributor `pubkey`; TLS for transport.
- Per-key rate limits; optional IP allowlists.
- On-chain anchoring publishes only high-level data (e.g. status of a link: healthy/incident/maintenance).

# **Backwards Compatibility**

- Versioned API; additive changes within major versions; deprecations have grace periods.
- Storage migrations are behind the gateway; clients unaffected.

# **Open Questions**

1. Are we already serving client-facing APIs? If so, can we build on top of those for incident/maintenance registration?

# Future expansions

- We could capture the slack conversations as artifacts at controlled points (on close, or time-based) and store it in airtable as well. Slack replies are not canonical. A bot could periodically snapshots each thread (e.g., on `closed` or every 6 hours for active sev1/sev2) and appends a read-only transcript to the ticket as an attachment and/or a consolidated timeline update.
- Slack file uploads could be mirrored as attachment as well.
- We could expand the following fields:
    
    
    | `status` | `open`, `acknowledged`, `investigating`, `mitigating`, `monitoring`, `resolved`, `closed`, `planned`, `in-progress`, `completed`, `cancelled` | **Incidents** typically exclude `planned/completed/cancelled`. **Maintenance** typically start `planned` → `in-progress` → `completed`/`closed`. |  |
    | --- | --- | --- | --- |
    | `labels` | Freeform tags | Optional | e.g., routing, core, firmware |
    | `assignee`  | contributor pubkey |  |  |