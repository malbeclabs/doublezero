# RFC - Network contributor incident and maintenance logging

# Summary

**Status: Approved**

This RFC proposes a gateway-first mechanism for creating and updating incidents and planned maintenance across the DoubleZero network. Contributors use a single, versioned API to publish operational events while retaining their internal tools. The gateway validates identity (API key bound to contributor), enforces enumerations, normalizes payloads, stores them in a  datastore, and notifies coordination channels in Slack.

Contributors can create incidents or maintenance via a shared form which submits through the API gateway, or directly through the API itself. Slack remains the primary interface for awareness, conversation, and history, all open and historical events appear in `#dz-contributor-incidents`* and `#dz-contributor-maintenance`*.

Over time, minimal facts are anchored on the DoubleZero ledger so any party can verify contributor-reported status or build independent explorers without relying on centralized databases.

# **Motivation**

Operational awareness across contributors is fragmented in private systems and Slack DMs / Threads. We need a uniform, verifiable way to publish incidents (unplanned) and maintenance (planned), with searchable shared context and cross-contributor visibility.

A unified gateway gives one authoritative ingress for operational events. Slack remains the collaboration surface, but the canonical record of status and lifecycle changes becomes machine-verifiable and future-proof for decentralization.

This design delivers immediate value (shared visibility, consistent status logs, network-wide coordination) while maintaining a clean migration path to onchain anchoring.

# **New Terminology**

| Term | Definition |
| --- | --- |
| **Incident** | Unplanned service-impacting event with enumerated severity and status. |
| **Maintenance** | Planned, time-bounded activity that may affect availability. Auto-closes after `end_at`. |
| **Proof of Contributor** | Caller authority via per-contributor API key (MVP); later, signed requests and onchain registry. |
| **Gateway** | Authenticated, versioned ingress that validates identity, enforces schema, persists events, and emits notifications. |

# **Alternatives Considered**

- **Do nothing:** Keeps fragmented visibility and manual coordination.
- **Point-to-point bridges:** N² maintenance trap across diverse contributor systems.
- **Mandate one product:** Simplifies normalization but undermines autonomy.
- **Go fully onchain day one:** Slows adoption and tooling readiness.

A **gateway-first** design achieves fast adoption and a clear path to progressive decentralization.

# **Detailed Design**

## Architecture

- **Ingress**: Versioned REST API and web form (writes through the API).
- **Identity**: API key bound to a Contributor `pubkey` (service key).
- **Storage**: PostgreSQL → onchain anchors.
- **Notifications**: Slack channel for new records, status changes (MVP).
- **Time Convention**: All timestamps are displayed and entered in UTC. The webform and incident tracking table show times in UTC.

## Cross-Contributor Visibility

- Contributors can only create tickets for their own devices and links.
- All tickets are visible to all contributors for network-wide awareness.
- Contributors can collaborate in Slack threads on any ticket notification.
- For issues involving another contributor's infrastructure, escalate to DZ/Malbeclabs (see DZX Link Workflow in Permission Model).

## Maintenance Events

- Fields include planned `start_at`, `end_at`, affected devices/links, and free-form notes.
- Auto-close: Maintenance transitions to `closed` 24 hours after `end_at`.
- Visible to all contributors to avoid overlapping maintenance windows.

## Visibility and Replies

- All contributors can view all open tickets (incidents + maintenance) and append updates/comments in slack threads.

## Commenting, Updates, and Attachments (Slack, API, Onchain)

**Design choice (MVP)**: Single ingress for structured facts (open ticket / create maintenance, close ticket / close maintenance) via API/webform; Slack used for human conversation and notifications.

**Why**:

- **Consistency**: Slack threads differ in structure, permissions, edits/deletes, and attachments. Bi-directional real-time sync introduces race conditions and partial failures.
- **Clarity**: One authoritative store simplifies future onchain anchoring and auditability.
- **Pragmatism**: Teams can still converse in Slack. Which makes it easy to share logs, screenshots or start a call to collaborate on a particular issue.

**How (MVP)**:

- The gateway forces the contributor to fill in all required fields and authenticates via an API key that is bound to the contributor. This makes sure we capture all required information which we could also use to start developing the onchain registration of incidents/maintenance.
- The webform displays human-readable device names and link codes in dropdowns for easy selection (contributors only see their own devices and links). The underlying pubkeys are stored but not shown to users.
- The gateway posts a Slack message per ticket and maintenance item in `#dz-contributor-incidents`* or `#dz-contributor-maintenance`*

## Identity Stages

- **Now (MVP)**: API key per contributor bound to `pubkey`.
- **Later**: Onchain registry binds `pubkey` to contributor.

## Contributor Onboarding

Contributors must complete the following steps to access the OPS Management portal:

**1. Set Ops Manager Key via DoubleZero CLI**
```
doublezero contributor update --ops-manager <OPS_MANAGER_PUBKEY> --pubkey <CONTRIBUTOR_PUBKEY>
```
The Ops Manager key is a Solana pubkey from a browser-compatible wallet. Supported wallets: Phantom, Solflare, Coinbase Wallet.

**2. Connect Wallet on OPS Management Portal**
- Navigate to the OPS Management page
- Connect wallet and sign a message to prove ownership of the Ops Manager private key
- Once authenticated, the Incident Tracking Table displays all open incidents and maintenance created by the contributor

**3. Create API Keys (Optional)**
- Click "Manage API Keys" to create API keys for programmatic access to the API
- API documentation is available for download from this page

**4. Create Incidents/Maintenance**
- Use the "Create New Record" button to manually create incidents or maintenance via the webform
- Alternatively, use the API directly with the generated API keys

## Permission Model

There are two types of authenticated users with different permission levels:

### Contributor Ops Manager Keys
Contributor ops manager keys are bound to a specific contributor and have limited permissions:
- Can create tickets for their own devices and links only
- Can assign tickets to themselves
- Can assign tickets to DZ/Malbeclabs (for escalation or support requests)
- Cannot assign tickets directly to other contributors

### Admin Keys (DZ/Malbeclabs)
Admin keys have elevated permissions for network-wide operations and support:
- Can create tickets for any contributor's devices and links
- Can assign tickets to any contributor
- Can reassign tickets between contributors

**Use Cases for Admin Keys:**
- DZ operations team managing network-wide incidents
- Receiving escalations from contributors for cross-contributor issues (e.g., DZX links involving multiple contributors)
- Receiving support requests for DZ-managed software (activator, controller, etc.)
- Reassigning tickets to the appropriate contributor when needed

**DZX Link Ownership:**
DZX links connect devices from two different contributors. The A-side device (first device in the link name) determines ownership. Only the A-side contributor can create incidents or maintenance for that DZX link.

Example: For link `deviceA:deviceB`, the contributor who owns `deviceA` owns the link and can create tickets for it.

**DZX Link Escalation Workflow:**
If an issue involves the B-side of a DZX link (owned by another contributor):
1. A-side contributor creates ticket for the DZX link they own
2. Contributor assigns (or reassigns) the ticket to DZ/Malbeclabs
3. DZ/Malbeclabs investigates and can reassign to the B-side contributor if needed

## Slack Notifications

Notifications are posted to `#dz-contributor-incidents`* and `#dz-contributor-maintenance`* channels. In the MVP, notifications do not include @mentions. Automated Slack user group tagging may be added in a future iteration.

*Slack channel names are illustrative. Actual channel names may vary by organization or deployment.

## Enumerations

| Field | Options | Notes |
| --- | --- | --- |
| `type` | `incident`, `maintenance` | Record category. |
| `status` | See Status Lifecycle section | Incidents and maintenance have different status flows. See Status Lifecycle section for details. |
| `severity` | `sev1`, `sev2`, `sev3` | Required for incidents; optional/default `sev3` for maintenance. |
| `root_cause` | See Root Cause Codes section | Incidents only. Can be set at any stage. Required when incident transitions to `resolved` or `closed`. |

## Severity Levels

Severity reflects impact to the DoubleZero network.

| Severity | Impact | Examples | Priority |
| --- | --- | --- | --- |
| `sev1` | <ul><li>Full user impact or complete outage.</li><li>Major control plane or data plane breakage with no fallback.</li></ul> | <ul><li>>10% of user traffic blackholed on DZ, no fallback to public internet.</li><li>>80% of user onboarding, connect, or disconnect attempts failing.</li><li>>20% of DZDs reporting interface errors.</li><li>Controller returning valid but incorrect configs to DZD agents.</li></ul> | <ul><li>Drop everything and work on this immediately, even outside working hours.</li><li>Contributor must escalate to DoubleZero Foundation immediately and any other Contributor as necessary.</li></ul> | 
| `sev2` | <ul><li>Partial but substantial user impact.</li><li>Degraded service where users may have fallback but functionality is impaired.</li><li>Control plane or observability significantly impaired.</li></ul> | <ul><li>>20% of users unable to send/receive traffic over DZ tunnels, but failing back to public internet.</li><li>0–10% of user traffic blackholed on DZ without fallback.</li><li>20–80% of new user onboarding, connect, or disconnect attempts failing.</li><li>>20% of config agents failing to apply DZD config.</li><li>0–20% of DZDs reporting interface errors.</li><li>Upstream issues causing observability loss (monitoring/alerting down).</li><li>Onchain data pipeline down or producing incorrect data.</li><li>>20% of internet latency collection or submission failing.</li><li>Controller inaccessible by DZD agents.</li><li>Controller returning invalid configs to DZDs that will not be applied.</li></ul> | <ul><li>Treat as urgent; prioritize resolution as soon as detected.</li><li>Contributor should actively coordinate and may escalate to DoubleZero Foundation and/or any other Contributor as necessary.</li><li>Overnight response required if degradation is sustained.</li></ul> | 
| `sev3` | <ul><li>Limited user impact or no user-visible impact.</li><li>Degraded service for a small fraction of users, or background system issues.</li><li>Clear potential for escalation if left unresolved.</li></ul> | <ul><li>0–20% of users unable to send/receive traffic over DZ tunnels, with fallback to public internet.</li><li>0–20% of DZDs reporting interface errors.</li><li>0–20% of DZDs experiencing config agent failures.</li><li>0–20% of user onboarding, connect, or disconnect attempts failing.</li><li>>20% of internet latency collection or submission failing for a single data provider.</li><li>0–20% of internet latency collection or submission failing for all data providers.</li><li>Bugs or tech debt causing alerting noise that cannot be silenced.</li><li>DIA down or ledger RPC networking issues for 0–20% of devices for several hours.</li><li>Low-impact issues such as minor bugs, cosmetic errors, or isolated incidents not affecting customer traffic.</li><li>Small fraction of devices intermittently reporting errors without service disruption.</li></ul> | <ul><li>Top priority during working hours.</li><li>Contributor may escalate to DoubleZero Foundation and/or any other Contributor as necessary if progress is blocked.</li><li>Monitor closely for signs of worsening impact.</li><li>Work during business hours; can be deferred if higher-priority incidents exist.</li><li>Escalation outside business hours is not required unless impact increases.</li></ul> |

## Status Lifecycle

### Incident Statuses

| Status | When to Use |
| --- | --- |
| `open` | Initial state when incident is reported. Something is wrong but no one has started working on it yet. |
| `acknowledged` | Someone has seen the incident and taken ownership. Signals to others: "this is being handled, don't duplicate effort." |
| `investigating` | Actively diagnosing root cause. Gathering logs, checking metrics, running tests, identifying what's broken. |
| `mitigating` | Root cause identified (or suspected), actively applying a fix or workaround. Traffic may still be impacted. |
| `monitoring` | Fix applied, watching to confirm it holds. Not yet confident the issue is fully resolved. |
| `resolved` | Issue is fixed and confirmed stable. Service restored. Ready for closure after any final documentation. |
| `closed` | Incident fully complete. Root cause assigned, post-mortem done if needed. No further action required. |

**Incident Flow:**
```
open → acknowledged → investigating → mitigating → monitoring → resolved → closed
  │         │              ↑                                               ↑
  │         └──────────────┘  (skip acknowledged if engineer jumps straight in)
  │                                                                        │
  └────────────────────────────────────────────────────────────────────────┘ (direct close possible, but use appropriate statuses throughout lifecycle)
```

### Maintenance Statuses

| Status | When to Use |
| --- | --- |
| `planned` | Maintenance is scheduled but hasn't started yet. Window is in the future. Other contributors can see and avoid conflicts. |
| `in-progress` | Maintenance window has begun. Work is actively being performed. Traffic may be impacted as announced. |
| `completed` | Work finished successfully. All planned changes applied. Monitoring health. |
| `closed` | Maintenance fully documented and archived. Final state after `completed`. |
| `cancelled` | Maintenance was called off before or during execution. No changes made, or changes were rolled back. |

**Maintenance Flow:**
```
planned → in-progress → completed → closed (auto 24h after end_at)
    ↓          ↓
    └──────────┴──→ cancelled
```

## Root Cause Codes

Root cause codes categorize the underlying cause of incidents. This field is only applicable to incidents and is not shown for maintenance. Root cause can be set at any stage of the incident lifecycle, but is required when an incident transitions to `resolved` or `closed`. This helps with post-incident analysis, identifying patterns, and tracking purposes.

| Code | Description |
| --- | --- |
| `hardware` | Hardware repair, replacement, or upgrade (SFP, NIC, cable, device). |
| `software` | Software or firmware fix, update, or restart. |
| `configuration` | Configuration change, fix, or rollback. |
| `capacity` | Congestion, capacity limits, or traffic management. |
| `carrier` | Circuit, wavelength, or cross-connect provider issue. |
| `network_external` | External network issue outside contributor control. |
| `facility` | Datacenter infrastructure issue (power, cooling). |
| `fiber_cut` | Physical fiber damage repaired. |
| `security` | Security incident mitigated. |
| `human_error` | Operational mistake corrected. |
| `false_positive` | No actual issue found after investigation. |
| `duplicate` | Already tracked in another ticket. |
| `self_resolved` | Issue resolved without intervention. |
| `dz_managed` | Issue with DoubleZero-managed software component (activator, controller, etc.). |

## Canonical Schema (v1) — **Tables and Fields**

### A. **Ticket** (Incident or Maintenance)

| Field | Description | Required | Notes | Later stored on chain | Updatable | Field validation |
| --- | --- | --- | --- | --- | --- | --- |
| `id` | System-assigned unique identifier | Auto | For incidents: `I<YYYYMMDD>-<xxxx>` (e.g., I20251217-af36). For maintenance: `M<YYYYMMDD>-<xxxx>` (e.g., M20251217-opub). | no | no | Auto generated, last 4 characters random |
| `type` | `incident` or `maintenance` | Yes | Drives required fields | yes | no | Enum: `incident`, `maintenance` |
| `title` | Short summary | Yes |  | no | yes | Max 100 characters |
| `description` | Detailed explanation | Yes |  | no | yes | Max 500 characters |
| `pubkey` | Creator contributor public key | Yes | Binds to API key | yes | no | Derived from API key |
| `reporter_name` | Human name (optional) | Optional | Captured from Portal/API | no | yes | Max 100 characters |
| `reporter_email` | Human email (optional) | Optional |  | no | yes | Valid email format, max 100 characters |
| `device_pubkey` | Devices implicated | yes* | List of pubkeys affected | yes | yes | Each element must be a registered device pubkey. No duplicates. |
| `affected_link_pubkey` | Links/assets implicated | yes* | List of pubkeys affected | yes | yes | Each element must be a registered link pubkey. No duplicates. |
| `severity` | Enumerated | Incidents | Optional/default for maintenance | no | yes | Enum: `sev1`, `sev2`, `sev3`. Required for incidents. Optional for maintenance (default `sev3`). |
| `status` | Enumerated | Yes |  | yes | yes | See Status Lifecycle section. Cannot set to terminal state on create. |
| `root_cause` | Enumerated | Incidents only | Not applicable to maintenance | no | yes | See Root Cause Codes section. Can be set at any stage. Required when incident status is `resolved` or `closed`. |
| `assignee` | Contributor or admin pubkey | Optional | Who is responsible for resolving | no | yes | Must be a valid contributor pubkey or admin pubkey. See Permission Model section for assignment rules. |
| `internal_reference` | Contributor's internal ticket/reference number | Optional | e.g., Jira, ServiceNow ticket ID | no | yes | Max 100 characters. Free-form text. |
| `start_at` | Start timestamp (UTC) | Maintenance: required | Incidents: visible in form, pre-filled with current time | yes | yes | Incidents: editable, defaults to creation time. Maintenance: required, can be in the past. |
| `end_at` | End timestamp (UTC) | Maintenance: required | Incidents: auto-set on close | yes | yes | Incidents: auto-set when closed. Maintenance: required, must be after `start_at`. |
| `auto_close_after_hours` | Hours after `end_at` to auto-close | Maintenance only | Default 24 hours | no | no | Default 24. Only applies to maintenance. |
| `created_at` / `updated_at` | Timestamps | Auto |  | yes | no | Auto-managed by system |

**At least one* `device_pubkey` *or one* `affected_link_pubkey` *is required to be filled in*

## Notifications

- **Create incident**: Post to `#dz-contributor-incidents`* with ticket id, status, severity, device and link pubkeys resolved to human readable device and link names, name of the contributor who created the incident, internal reference (if provided).
- **Create maintenance**: Post to `#dz-contributor-maintenance`* with ticket id, status, device and link pubkeys resolved to human readable device and link names, planned start time and planned end time, name of the contributor who created the maintenance, internal reference (if provided).
- **Assign ticket**: Update the Slack message banner to show assignee. Post a thread reply with assignment update (e.g., "Assigned to Acme Networks").
- **Status change**: Post update to Slack thread with status transition and who made the change.
- **Close incident/maintenance**: Update the Slack message to reflect closed status. Post thread update with resolution details and root cause (for incidents).

## Storage and Lifecycle

- **PostgreSQL**: Primary datastore for incidents and maintenance records.
- **Maintenance auto-close**: Close 24 hours after `end_at`.
- **Incidents**: Manual close, via form or direct via the API.
- **Slack**: should follow our internal data retention policy.

## Going Fully Onchain: Trade-offs

- **Pros**: Public verifiability; replayable audit; independent explorers; resilience.
- **Cons**: Higher engineering lift now; comments/attachments require off-chain CAS; Rich media UX still need portals.

**Path**: Use the API gateway + Slack now for fast adoption. In parallel, build the onchain registration of incidents/maintenance. 

# **Impact**

- Immediate, shared visibility for incidents and maintenance.
- Cross-contributor visibility and possibility to collaborate.
- Dedicated Slack channels for contributor incidents and contributor maintenance.
- Slack remains useful for collaboration, sharing rich media and visibility.
- Clear migration path to verifiable, onchain anchors.

# **Security Considerations**

- Authentication via API key; TLS for transport.
- Per-key rate limits;
- Onchain anchoring publishes only high-level data (e.g. status of a link: healthy/incident/maintenance).

# **Backwards Compatibility**

- Versioned API; additive changes within major versions; deprecations have grace periods.
- Storage migrations are behind the gateway; clients unaffected.

# Future expansions

- We could capture the slack conversations as artifacts at controlled points (on close, or time-based) and store them in the database. Slack replies are not canonical. A bot could periodically snapshot each thread (e.g., on `closed` or every 6 hours for active sev1/sev2) and append a read-only transcript to the ticket as an attachment and/or a consolidated timeline update.
- Slack file uploads could be mirrored as attachments as well.