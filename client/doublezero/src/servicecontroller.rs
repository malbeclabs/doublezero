//! Binary-local daemon client retained for the not-yet-migrated multicast
//! verbs (`subscribe`/`unsubscribe`/`publish`/`unpublish`), which only need
//! the daemon-discovered client IP via `/v2/status`. The full daemon client
//! lives in `doublezero-daemon-cli` (`DaemonClient`); this remnant moves there
//! when the multicast verbs migrate (RFC-20).

use chrono::DateTime;
use eyre::eyre;
use http_body_util::{BodyExt, Empty};
use hyper::{body::Bytes, Method, Request};
use hyper_util::{client::legacy::Client, rt::TokioExecutor};
use hyperlocal::{UnixConnector, Uri};
use serde::{Deserialize, Serialize};
use std::sync::OnceLock;
use tabled::{derive::display, Tabled};

const DEFAULT_SOCKET_PATH: &str = "/var/run/doublezerod/doublezerod.sock";
static GLOBAL_SOCKET_PATH: OnceLock<String> = OnceLock::new();

#[derive(Tabled, Serialize, Deserialize, Debug, Clone)]
#[tabled(display(Option, "display::option", ""))]
pub struct StatusResponse {
    #[tabled(inline)]
    pub doublezero_status: DoubleZeroStatus,
    #[tabled(rename = "Tunnel Name")]
    pub tunnel_name: Option<String>,
    #[tabled(rename = "Tunnel Src")]
    pub tunnel_src: Option<String>,
    #[tabled(rename = "Tunnel Dst")]
    pub tunnel_dst: Option<String>,
    #[tabled(rename = "Doublezero IP")]
    pub doublezero_ip: Option<String>,
    #[tabled(rename = "User Type")]
    pub user_type: Option<String>,
}

#[derive(Tabled, Serialize, Deserialize, Debug, Clone)]
pub struct DoubleZeroStatus {
    #[tabled(rename = "Tunnel Status")]
    pub session_status: String,
    #[tabled(rename = "Last Session Update", display = "maybe_i64_to_dt_str")]
    pub last_session_update: Option<i64>,
}

fn maybe_i64_to_dt_str(maybe_i64_dt: &Option<i64>) -> String {
    maybe_i64_dt.as_ref().map_or_else(
        || "no session data".to_string(),
        |dt_i64| {
            DateTime::from_timestamp(*dt_i64, 0)
                .map(|dt| dt.to_string())
                .unwrap_or_else(|| "invalid timestamp".to_string())
        },
    )
}

#[derive(Serialize, Deserialize, Debug, Clone, Default, PartialEq)]
pub struct MulticastGroups {
    #[serde(default)]
    pub publisher: Vec<String>,
    #[serde(default)]
    pub subscriber: Vec<String>,
}

/// A single multicast group the user participates in, with the group's onchain
/// details and the user's role(s). A user that is both publisher and subscriber
/// of a group appears once with both booleans set.
#[derive(Serialize, Deserialize, Debug, Clone, Default, PartialEq, Eq)]
pub struct Subscription {
    #[serde(default)]
    pub pubkey: String,
    #[serde(default)]
    pub code: String,
    #[serde(default)]
    pub multicast_ip: String,
    #[serde(default)]
    pub max_bandwidth: u64,
    #[serde(default)]
    pub publisher: bool,
    #[serde(default)]
    pub subscriber: bool,
}

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
    #[serde(default)]
    pub subscriptions: Vec<Subscription>,
}

#[derive(Serialize, Deserialize, Debug)]
pub struct V2StatusResponse {
    pub reconciler_enabled: bool,
    #[serde(default)]
    pub client_ip: String,
    #[serde(default)]
    pub network: String,
    pub services: Vec<V2ServiceStatus>,
}

#[allow(async_fn_in_trait)]
pub trait ServiceController {
    async fn v2_status(&self) -> eyre::Result<V2StatusResponse>;
}

pub struct ServiceControllerImpl {
    pub socket_path: String,
}

impl ServiceControllerImpl {
    pub fn set_global_socket_path(socket_path: impl Into<String>) {
        let _ = GLOBAL_SOCKET_PATH.set(socket_path.into());
    }

    pub fn new(socket_path: Option<String>) -> ServiceControllerImpl {
        ServiceControllerImpl {
            socket_path: socket_path.unwrap_or_else(|| {
                GLOBAL_SOCKET_PATH
                    .get()
                    .cloned()
                    .unwrap_or_else(|| DEFAULT_SOCKET_PATH.to_string())
            }),
        }
    }
}

impl ServiceController for ServiceControllerImpl {
    async fn v2_status(&self) -> eyre::Result<V2StatusResponse> {
        let client = Client::builder(TokioExecutor::new()).build(UnixConnector);
        let req = Request::builder()
            .method(Method::GET)
            .uri(Uri::new(&self.socket_path, "/v2/status"))
            .body(Empty::<Bytes>::new())?;
        let res = client
            .request(req)
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {e}"))?;
        let data = res.into_body().collect().await?.to_bytes();
        let response = serde_json::from_slice::<V2StatusResponse>(&data)
            .map_err(|e| eyre!("Unable to parse V2StatusResponse: {e}"))?;
        Ok(response)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Test StatusResponse JSON output format
    #[test]
    fn test_status_response_json_output_format() {
        let status = StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "BGP Session Up".to_string(),
                last_session_update: Some(1_625_247_600),
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("10.0.0.1".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()),
            doublezero_ip: Some("10.1.2.3".to_string()),
            user_type: Some("IBRL".to_string()),
        };

        let json_output = serde_json::to_value(&status).expect("Failed to serialize");

        // Validate all fields are present
        assert!(
            json_output.get("doublezero_status").is_some(),
            "Missing 'doublezero_status' field"
        );
        assert!(
            json_output.get("tunnel_name").is_some(),
            "Missing 'tunnel_name' field"
        );
        assert!(
            json_output.get("tunnel_src").is_some(),
            "Missing 'tunnel_src' field"
        );
        assert!(
            json_output.get("tunnel_dst").is_some(),
            "Missing 'tunnel_dst' field"
        );
        assert!(
            json_output.get("doublezero_ip").is_some(),
            "Missing 'doublezero_ip' field"
        );
        assert!(
            json_output.get("user_type").is_some(),
            "Missing 'user_type' field"
        );

        // Validate nested doublezero_status fields
        let dz_status = json_output.get("doublezero_status").unwrap();
        assert!(
            dz_status.get("session_status").is_some(),
            "Missing 'session_status' field"
        );
        assert!(
            dz_status.get("last_session_update").is_some(),
            "Missing 'last_session_update' field"
        );

        // Validate field values
        assert_eq!(json_output.get("tunnel_name").unwrap(), "doublezero1");
        assert_eq!(json_output.get("tunnel_src").unwrap(), "10.0.0.1");
        assert_eq!(json_output.get("tunnel_dst").unwrap(), "5.6.7.8");
        assert_eq!(json_output.get("doublezero_ip").unwrap(), "10.1.2.3");
        assert_eq!(json_output.get("user_type").unwrap(), "IBRL");
        assert_eq!(dz_status.get("session_status").unwrap(), "BGP Session Up");
        assert_eq!(dz_status.get("last_session_update").unwrap(), 1_625_247_600);
    }
}
