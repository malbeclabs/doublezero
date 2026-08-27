# Status Command: Show Multicast Group Memberships

## Goal

Show which multicast groups a user is subscribed to and/or publishing to in the `doublezero status` command output.

## Current Behavior

The `doublezero status` command shows tunnel status, device info, metro, network, and tenant. For multicast user types, there is no indication of which groups the user belongs to or their role (publisher vs subscriber).

## Design

### Data flow

The daemon already fetches full onchain program data during reconciliation, including `MulticastGroup` accounts (with group codes) and `User` accounts (with `Publishers` and `Subscribers` pubkey vecs). The multicast group information just needs to be threaded through the v2 status response.

### Go daemon changes (`client/doublezerod/`)

Add a `MulticastGroups` struct and a `multicast_groups` field to `V2ServiceStatus`:

```go
type MulticastGroups struct {
    Publisher  []string `json:"publisher"`
    Subscriber []string `json:"subscriber"`
}

type V2ServiceStatus struct {
    *api.StatusResponse
    // ... existing fields ...
    MulticastGroups MulticastGroups `json:"multicast_groups"`
}
```

In `enrichStatuses()`, for each service, resolve the matched user's `Publishers` and `Subscribers` pubkey vecs against the fetched `MulticastGroup` map to populate the group code lists. Non-multicast users get empty lists.

### Rust CLI changes (`client/doublezero/`)

Extend `V2ServiceStatus` in `servicecontroller.rs`:

```rust
#[derive(Serialize, Deserialize, Debug, Clone, Default)]
pub struct MulticastGroups {
    #[serde(default)]
    pub publisher: Vec<String>,
    #[serde(default)]
    pub subscriber: Vec<String>,
}
```

Add `multicast_groups: MulticastGroups` to `V2ServiceStatus` (with `#[serde(default)]` for backward compatibility with older daemons).

In `status.rs`, add a "Multicast Groups" column to `AppendedStatusResponse` that formats as `P:code1,S:code2` in table mode. JSON mode includes the structured `multicast_groups` object.

### Output examples

**Table mode:**
```
| Session Status | ... | Multicast Groups         |
|----------------|-----|--------------------------|
| BGP Session Up | ... | P:solana-lv,S:solana-ams |
```

**JSON mode:**
```json
{
  "multicast_groups": {
    "publisher": ["solana-lv"],
    "subscriber": ["solana-ams"]
  }
}
```

For non-multicast users, the column is empty and both JSON arrays are `[]`.

## Testing

- Unit tests in `status.rs` for the new field formatting (table and JSON)
- Unit test for backward compatibility when daemon omits the field (serde default)
- Verify `enrichStatuses` correctly resolves publisher/subscriber pubkeys to group codes in Go tests
