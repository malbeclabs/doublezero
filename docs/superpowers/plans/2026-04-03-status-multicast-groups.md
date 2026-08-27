# Status Command: Multicast Group Memberships — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show which multicast groups a user publishes to / subscribes to in the `doublezero status` command output.

**Architecture:** Extend the Go daemon's `V2ServiceStatus` with a `multicast_groups` object containing `publisher` and `subscriber` string arrays (group codes). The `enrichStatuses()` function already has access to the matched user and onchain multicast group data — it just needs to resolve the user's pubkey vecs to group codes. The Rust CLI then deserializes and displays the new field.

**Tech Stack:** Go (daemon), Rust (CLI), serde, tabled

---

## File Structure

| Action | File | Responsibility |
|--------|------|---------------|
| Modify | `client/doublezerod/internal/manager/http.go` | Add `MulticastGroups` struct and field to `V2ServiceStatus`; populate in `enrichStatuses()` |
| Modify | `client/doublezerod/internal/manager/reconciler_test.go` | Add multicast group assertions to `TestServeV2Status_Enrichment` |
| Modify | `client/doublezero/src/servicecontroller.rs` | Add `MulticastGroups` struct and field to `V2ServiceStatus` |
| Modify | `client/doublezero/src/command/status.rs` | Add multicast groups column to display; format as `P:code,S:code` |

---

### Task 1: Go daemon — add MulticastGroups to V2ServiceStatus and populate in enrichStatuses

**Files:**
- Modify: `client/doublezerod/internal/manager/http.go:17-26` (struct definitions)
- Modify: `client/doublezerod/internal/manager/http.go:226-367` (enrichStatuses)
- Modify: `client/doublezerod/internal/manager/reconciler_test.go:1205-1397` (TestServeV2Status_Enrichment)

- [ ] **Step 1: Write the failing test**

In `client/doublezerod/internal/manager/reconciler_test.go`, update the `wantService` struct and test cases in `TestServeV2Status_Enrichment` to assert on multicast groups.

First, add `Code` to the existing `mcastGroup` fixture (around line 1218):

```go
mcastGroup := serviceability.MulticastGroup{
    PubKey:      mcastGroupPK,
    MulticastIp: [4]uint8{239, 0, 0, 1},
    Code:        "solana-ams",
}
```

Then add fields to the `wantService` struct (around line 1223):

```go
type wantService struct {
    userType      string
    currentDevice string
    metro         string
    tenant        string
    hasDzIP       bool
    pubGroups     []string
    subGroups     []string
}
```

Update each test case's `want` entries. For `ibrl_only`:
```go
want: []wantService{
    {userType: "IBRL", currentDevice: "dz1", metro: "Amsterdam", tenant: "acme", hasDzIP: true, pubGroups: nil, subGroups: nil},
},
```

For `multicast_publisher`:
```go
want: []wantService{
    {userType: "Multicast", currentDevice: "dz1", metro: "Amsterdam", tenant: "acme", hasDzIP: true, pubGroups: []string{"solana-ams"}, subGroups: nil},
},
```

For `multicast_subscriber`:
```go
want: []wantService{
    {userType: "Multicast", currentDevice: "dz1", metro: "Amsterdam", tenant: "acme", hasDzIP: false, pubGroups: nil, subGroups: []string{"solana-ams"}},
},
```

For `ibrl_plus_multicast_subscriber`:
```go
want: []wantService{
    {userType: "IBRL", currentDevice: "dz1", metro: "Amsterdam", tenant: "acme", hasDzIP: true, pubGroups: nil, subGroups: nil},
    {userType: "Multicast", currentDevice: "dz1", metro: "Amsterdam", tenant: "acme", hasDzIP: false, pubGroups: nil, subGroups: []string{"solana-ams"}},
},
```

For `ibrl_plus_multicast_publisher`:
```go
want: []wantService{
    {userType: "IBRL", currentDevice: "dz1", metro: "Amsterdam", tenant: "acme", hasDzIP: true, pubGroups: nil, subGroups: nil},
    {userType: "Multicast", currentDevice: "dz1", metro: "Amsterdam", tenant: "acme", hasDzIP: true, pubGroups: []string{"solana-ams"}, subGroups: nil},
},
```

Add assertions at the end of the test loop (after the existing `hasDzIP` check, around line 1394):

```go
if !slices.Equal(svc.MulticastGroups.Publisher, w.pubGroups) {
    t.Errorf("[%s] expected pub groups %v, got %v", w.userType, w.pubGroups, svc.MulticastGroups.Publisher)
}
if !slices.Equal(svc.MulticastGroups.Subscriber, w.subGroups) {
    t.Errorf("[%s] expected sub groups %v, got %v", w.userType, w.subGroups, svc.MulticastGroups.Subscriber)
}
```

Add `"slices"` to the import block if not already present.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/ben/src/malbec/doublezero && go test -run TestServeV2Status_Enrichment -v ./client/doublezerod/internal/manager/...`

Expected: Compilation error — `V2ServiceStatus` has no `MulticastGroups` field.

- [ ] **Step 3: Implement the Go daemon changes**

In `client/doublezerod/internal/manager/http.go`, add the `MulticastGroups` struct and field.

After the existing `V2ServiceStatus` struct (line 26), add:

```go
// MulticastGroups contains the group codes a user publishes to and subscribes to.
type MulticastGroups struct {
	Publisher  []string `json:"publisher"`
	Subscriber []string `json:"subscriber"`
}
```

Add the field to `V2ServiceStatus`:

```go
type V2ServiceStatus struct {
	*api.StatusResponse
	CurrentDevice               string          `json:"current_device"`
	CurrentDeviceRttNanoseconds int64           `json:"current_device_rtt_nanoseconds,omitempty"`
	CurrentDeviceLossPercentage float64         `json:"current_device_loss_percentage,omitempty"`
	LowestLatencyDevice         string          `json:"lowest_latency_device"`
	Metro                       string          `json:"metro"`
	Tenant                      string          `json:"tenant"`
	MulticastGroups             MulticastGroups `json:"multicast_groups"`
}
```

In `enrichStatuses()`, build a multicast group lookup map. After the existing `tenantsByPK` map (around line 266), add:

```go
mcastGroupsByPK := make(map[[32]byte]serviceability.MulticastGroup, len(data.MulticastGroups))
for _, mg := range data.MulticastGroups {
    mcastGroupsByPK[mg.PubKey] = mg
}
```

After the tenant enrichment block (after line 356, before the lowest latency computation), add:

```go
if matchedUser != nil {
    for _, pk := range matchedUser.Publishers {
        if mg, ok := mcastGroupsByPK[pk]; ok {
            es.MulticastGroups.Publisher = append(es.MulticastGroups.Publisher, mg.Code)
        }
    }
    for _, pk := range matchedUser.Subscribers {
        if mg, ok := mcastGroupsByPK[pk]; ok {
            es.MulticastGroups.Subscriber = append(es.MulticastGroups.Subscriber, mg.Code)
        }
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/ben/src/malbec/doublezero && go test -run TestServeV2Status_Enrichment -v ./client/doublezerod/internal/manager/...`

Expected: PASS

- [ ] **Step 5: Run full Go test suite for the package**

Run: `cd /Users/ben/src/malbec/doublezero && go test -v ./client/doublezerod/internal/manager/...`

Expected: All tests pass. The new field serializes as empty arrays `{"publisher":null,"subscriber":null}` for non-multicast users, which is fine — the Rust side uses `#[serde(default)]`.

- [ ] **Step 6: Commit**

```
client/daemon: add multicast groups to v2 status response
```

---

### Task 2: Rust CLI — deserialize and display multicast groups

**Files:**
- Modify: `client/doublezero/src/servicecontroller.rs:149-161` (V2ServiceStatus struct)
- Modify: `client/doublezero/src/command/status.rs:20-36` (AppendedStatusResponse struct)
- Modify: `client/doublezero/src/command/status.rs:51-131` (command_impl)

- [ ] **Step 1: Write the failing test**

In `client/doublezero/src/command/status.rs`, add a new test for multicast group display. Add after the existing `test_status_command_multicast_subscriber` test (after line 354):

```rust
#[tokio::test]
async fn test_status_command_multicast_groups_display() {
    let mock_command = MockCliCommand::new();
    let mut mock_controller = MockServiceController::new();

    mock_controller.expect_v2_status().returning(|| {
        Ok(V2StatusResponse {
            reconciler_enabled: true,
            client_ip: String::new(),
            network: "testnet".to_string(),
            services: vec![V2ServiceStatus {
                status: StatusResponse {
                    doublezero_status: DoubleZeroStatus {
                        session_status: "BGP Session Up".to_string(),
                        last_session_update: Some(1625247600),
                    },
                    tunnel_name: Some("doublezero1".to_string()),
                    tunnel_src: Some("10.10.10.10".to_string()),
                    tunnel_dst: Some("5.6.7.8".to_string()),
                    doublezero_ip: None,
                    user_type: Some("Multicast".to_string()),
                },
                current_device: "device1".to_string(),
                lowest_latency_device: "device1".to_string(),
                metro: "metro".to_string(),
                tenant: String::new(),
                multicast_groups: MulticastGroups {
                    publisher: vec!["solana-lv".to_string()],
                    subscriber: vec!["solana-ams".to_string()],
                },
            }],
        })
    });

    let result = StatusCliCommand { json: true }
        .command_impl(&mock_command, &mock_controller)
        .await;

    assert!(result.is_ok());
    let result = result.unwrap();
    assert_eq!(result.len(), 1);
    assert_eq!(result[0].multicast_groups, "P:solana-lv,S:solana-ams");
}
```

Also add a test for backward compatibility (daemon doesn't send the field):

```rust
#[test]
fn test_multicast_groups_serde_default() {
    let json = r#"{
        "doublezero_status": {"session_status": "BGP Session Up", "last_session_update": null},
        "tunnel_name": null, "tunnel_src": null, "tunnel_dst": null,
        "doublezero_ip": null, "user_type": "IBRL",
        "current_device": "dz1", "lowest_latency_device": "dz1",
        "metro": "ams", "tenant": ""
    }"#;
    let svc: V2ServiceStatus = serde_json::from_str(json).unwrap();
    assert!(svc.multicast_groups.publisher.is_empty());
    assert!(svc.multicast_groups.subscriber.is_empty());
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/ben/src/malbec/doublezero && cargo test -p doublezero test_status_command_multicast_groups_display`

Expected: Compilation error — `MulticastGroups` type doesn't exist, `V2ServiceStatus` has no `multicast_groups` field.

- [ ] **Step 3: Add MulticastGroups struct to servicecontroller.rs**

In `client/doublezero/src/servicecontroller.rs`, add the struct before `V2ServiceStatus` (before line 149):

```rust
#[derive(Serialize, Deserialize, Debug, Clone, Default, PartialEq)]
pub struct MulticastGroups {
    #[serde(default)]
    pub publisher: Vec<String>,
    #[serde(default)]
    pub subscriber: Vec<String>,
}
```

Add the field to `V2ServiceStatus` (after the `tenant` field):

```rust
#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct V2ServiceStatus {
    #[serde(flatten)]
    pub status: StatusResponse,
    #[serde(default)]
    pub current_device: String,
    #[serde(default)]
    pub lowest_latency_device: String,
    #[serde(default)]
    pub metro: String,
    #[serde(default)]
    pub tenant: String,
    #[serde(default)]
    pub multicast_groups: MulticastGroups,
}
```

- [ ] **Step 4: Add multicast_groups to AppendedStatusResponse and format in command_impl**

In `client/doublezero/src/command/status.rs`, add the field to `AppendedStatusResponse`:

```rust
#[derive(Tabled, Debug, Deserialize, Serialize)]
struct AppendedStatusResponse {
    #[tabled(inline)]
    response: StatusResponse,
    #[tabled(rename = "Reconciler")]
    reconciler_enabled: bool,
    #[tabled(rename = "Tenant")]
    tenant: String,
    #[tabled(rename = "Current Device")]
    current_device: String,
    #[tabled(rename = "Lowest Latency Device")]
    lowest_latency_device: String,
    #[tabled(rename = "Metro")]
    metro: String,
    #[tabled(rename = "Network")]
    network: String,
    #[tabled(rename = "Multicast Groups")]
    multicast_groups: String,
}
```

Add `use crate::servicecontroller::MulticastGroups;` to the imports at the top of the file (add to the existing `use crate::servicecontroller::{...}` import).

Add a helper function to format multicast groups (before the `impl StatusCliCommand` block):

```rust
fn format_multicast_groups(groups: &MulticastGroups) -> String {
    let mut parts = Vec::new();
    for code in &groups.publisher {
        parts.push(format!("P:{code}"));
    }
    for code in &groups.subscriber {
        parts.push(format!("S:{code}"));
    }
    parts.join(",")
}
```

In `command_impl`, populate the new field when building each `AppendedStatusResponse`. In the empty-services branch (around line 62), add `multicast_groups: String::new()` to the struct literal.

In the main loop (around line 119), add the field:

```rust
responses.push(AppendedStatusResponse {
    response: svc.status.clone(),
    reconciler_enabled: v2_status.reconciler_enabled,
    current_device,
    lowest_latency_device,
    metro,
    network: network.clone(),
    tenant: svc.tenant.clone(),
    multicast_groups: format_multicast_groups(&svc.multicast_groups),
});
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /Users/ben/src/malbec/doublezero && cargo test -p doublezero test_status_command_multicast_groups_display test_multicast_groups_serde_default`

Expected: PASS

- [ ] **Step 6: Fix existing tests**

The existing tests in `status.rs` need the new `multicast_groups` field added to `V2ServiceStatus` struct literals and `AppendedStatusResponse` assertions. For each existing test that constructs a `V2ServiceStatus`, add:

```rust
multicast_groups: MulticastGroups::default(),
```

For tests using the `make_v2_service` helper, update the helper to include the field:

```rust
fn make_v2_service(
    // ... existing params ...
) -> V2ServiceStatus {
    V2ServiceStatus {
        status: StatusResponse { ... },
        current_device: current_device.to_string(),
        lowest_latency_device: lowest_latency_device.to_string(),
        metro: metro.to_string(),
        tenant: tenant.to_string(),
        multicast_groups: MulticastGroups::default(),
    }
}
```

For tests that assert on `AppendedStatusResponse` fields (like `test_status_json_output_format`), add `multicast_groups: String::new()` to the struct literal and add a JSON field assertion:

```rust
assert!(
    status.get("multicast_groups").is_some(),
    "Missing 'multicast_groups' field"
);
```

- [ ] **Step 7: Run full test suite**

Run: `cd /Users/ben/src/malbec/doublezero && cargo test -p doublezero`

Expected: All tests pass.

- [ ] **Step 8: Format**

Run: `cd /Users/ben/src/malbec/doublezero && make rust-fmt`

- [ ] **Step 9: Commit**

```
client/cli: show multicast groups in status command
```
