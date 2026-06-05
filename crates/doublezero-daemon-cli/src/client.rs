//! Mockable daemon-client trait wrapping HTTP-over-Unix-socket calls to
//! `doublezerod`, plus all request/response types shared across daemon verbs.

use chrono::DateTime;
use doublezero_config::Environment;
use eyre::eyre;
use http_body_util::{BodyExt, Empty, Full};
use hyper::{body::Bytes, Method, Request};
use hyper_util::{client::legacy::Client, rt::TokioExecutor};
use hyperlocal::{UnixConnector, Uri};
use mockall::automock;
use serde::{Deserialize, Serialize};
use std::{fmt, fs::File, path::Path, sync::OnceLock};
use tabled::{derive::display, Tabled};

pub(crate) const DEFAULT_SOCKET_PATH: &str = "/var/run/doublezerod/doublezerod.sock";
const NANOS_TO_MS: f64 = 1000000.0;
static GLOBAL_SOCKET_PATH: OnceLock<String> = OnceLock::new();

// ---------------------------------------------------------------------------
// Response / request types
// ---------------------------------------------------------------------------

#[derive(Clone, Tabled, Deserialize, Serialize, Debug)]
pub struct LatencyRecord {
    #[tabled(rename = "Pubkey")]
    pub device_pk: String,
    #[tabled(rename = "Code")]
    pub device_code: String,
    #[tabled(rename = "IP")]
    pub device_ip: String,
    #[tabled(display = "display_as_ms", rename = "Min")]
    pub min_latency_ns: i64,
    #[tabled(display = "display_as_ms", rename = "Max")]
    pub max_latency_ns: i64,
    #[tabled(display = "display_as_ms", rename = "Avg")]
    pub avg_latency_ns: i64,
    pub reachable: bool,
}

fn display_as_ms(latency: &i64) -> String {
    format!("{:.2}ms", (*latency as f64 / NANOS_TO_MS))
}

impl fmt::Display for LatencyRecord {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "device: {}, code: {}, ip: {}, latency min: {}, max: {}, avg: {}, reachable: {}",
            self.device_pk,
            self.device_code,
            self.device_ip,
            self.min_latency_ns,
            self.max_latency_ns,
            self.avg_latency_ns,
            self.reachable
        )
    }
}

#[derive(Deserialize, Debug)]
pub struct LatencyResponse {
    pub ready: bool,
    pub results: Vec<LatencyRecord>,
}

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

#[derive(Serialize, Deserialize, Debug)]
pub struct GetConfigResponse {
    pub program_id: String,
    pub rpc_url: String,
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

#[derive(Clone, Tabled, Deserialize, Serialize, Debug, PartialEq)]
#[tabled(display(Option, "display::option", ""))]
pub struct RouteRecord {
    #[tabled(rename = "Network")]
    pub network: String,
    #[tabled(rename = "Local IP")]
    pub local_ip: String,
    #[tabled(rename = "Peer IP")]
    pub peer_ip: String,
    #[tabled(rename = "Kernel State")]
    pub kernel_state: String,
    #[tabled(rename = "Liveness Last Updated")]
    pub liveness_last_updated: Option<String>,
    #[tabled(rename = "Liveness State")]
    pub liveness_state: Option<String>,
    #[tabled(rename = "Liveness State Reason")]
    pub liveness_state_reason: Option<String>,
    #[tabled(rename = "Peer Client Version")]
    pub peer_client_version: Option<String>,
}

impl fmt::Display for RouteRecord {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "local_ip: {}, peer_ip: {}", self.local_ip, self.peer_ip)
    }
}

#[derive(Deserialize, Debug)]
pub struct ErrorResponse {
    pub status: String,
    pub description: String,
}

/// Parse a daemon response, falling back to ErrorResponse if the primary type fails.
fn parse_daemon_response<T: serde::de::DeserializeOwned>(
    data: &[u8],
    endpoint: &str,
) -> eyre::Result<T> {
    match serde_json::from_slice::<T>(data) {
        Ok(response) => Ok(response),
        Err(parse_err) => match serde_json::from_slice::<ErrorResponse>(data) {
            Ok(err_resp) if err_resp.status == "error" => Err(eyre!(err_resp.description)),
            _ => Err(eyre!(
                "Failed to parse daemon {endpoint} response: {parse_err}"
            )),
        },
    }
}

#[derive(Serialize, Deserialize, Debug, Clone, Default, PartialEq)]
pub struct MulticastGroups {
    #[serde(default)]
    pub publisher: Vec<String>,
    #[serde(default)]
    pub subscriber: Vec<String>,
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
}

#[derive(Serialize, Deserialize, Debug, Clone)]
pub struct V2StatusResponse {
    pub reconciler_enabled: bool,
    #[serde(default)]
    pub client_ip: String,
    #[serde(default)]
    pub network: String,
    pub services: Vec<V2ServiceStatus>,
}

// ---------------------------------------------------------------------------
// DaemonClient trait (renamed from ServiceController)
// ---------------------------------------------------------------------------

#[allow(async_fn_in_trait)]
#[automock]
pub trait DaemonClient: Send + Sync {
    fn daemon_check(&self) -> bool;
    fn daemon_can_open(&self) -> bool;
    async fn get_config(&self) -> eyre::Result<GetConfigResponse>;
    async fn get_env(&self) -> eyre::Result<Environment>;
    async fn latency(&self) -> eyre::Result<LatencyResponse>;
    async fn status(&self) -> eyre::Result<Vec<StatusResponse>>;
    async fn v2_status(&self) -> eyre::Result<V2StatusResponse>;
    async fn enable(&self) -> eyre::Result<()>;
    async fn disable(&self) -> eyre::Result<()>;
    async fn routes(&self) -> eyre::Result<Vec<RouteRecord>>;
}

// ---------------------------------------------------------------------------
// Concrete implementation (Unix-socket HTTP)
// ---------------------------------------------------------------------------

pub struct DaemonClientImpl {
    pub socket_path: String,
}

impl DaemonClientImpl {
    pub fn set_global_socket_path(socket_path: impl Into<String>) {
        let _ = GLOBAL_SOCKET_PATH.set(socket_path.into());
    }

    pub fn new(socket_path: Option<String>) -> DaemonClientImpl {
        DaemonClientImpl {
            socket_path: socket_path.unwrap_or_else(|| {
                GLOBAL_SOCKET_PATH
                    .get()
                    .cloned()
                    .unwrap_or_else(|| DEFAULT_SOCKET_PATH.to_string())
            }),
        }
    }
}

impl DaemonClient for DaemonClientImpl {
    fn daemon_check(&self) -> bool {
        Path::new(&self.socket_path).exists()
    }

    fn daemon_can_open(&self) -> bool {
        let file = File::options()
            .read(true)
            .write(true)
            .open(&self.socket_path);
        match file {
            Ok(_) => true,
            Err(e) => !matches!(e.kind(), std::io::ErrorKind::PermissionDenied),
        }
    }

    async fn get_config(&self) -> eyre::Result<GetConfigResponse> {
        let uri = Uri::new(&self.socket_path, "/config").into();
        let client: Client<UnixConnector, Full<Bytes>> =
            Client::builder(TokioExecutor::new()).build(UnixConnector);
        let res = client
            .get(uri)
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {e}"))?;
        let data = res
            .into_body()
            .collect()
            .await
            .map_err(|e| eyre!("Unable to read response body: {e}"))?
            .to_bytes();
        parse_daemon_response::<GetConfigResponse>(&data, "/config")
    }

    async fn get_env(&self) -> eyre::Result<Environment> {
        let config = self.get_config().await?;
        Ok(Environment::from_program_id(&config.program_id).unwrap_or_default())
    }

    async fn latency(&self) -> eyre::Result<LatencyResponse> {
        let uri = Uri::new(&self.socket_path, "/v2/latency").into();
        let client: Client<UnixConnector, Full<Bytes>> =
            Client::builder(TokioExecutor::new()).build(UnixConnector);
        let res = client
            .get(uri)
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {e}"))?;
        let data = res
            .into_body()
            .collect()
            .await
            .map_err(|e| eyre!("Unable to read response body: {e}"))?
            .to_bytes();
        parse_daemon_response::<LatencyResponse>(&data, "/v2/latency")
    }

    async fn status(&self) -> eyre::Result<Vec<StatusResponse>> {
        let client = Client::builder(TokioExecutor::new()).build(UnixConnector);
        let req = Request::builder()
            .method(Method::GET)
            .uri(Uri::new(&self.socket_path, "/status"))
            .body(Empty::<Bytes>::new())?;
        match client.request(req).await {
            Ok(res) => {
                if res.status() != 200 {
                    eyre::bail!("Unable to connect to doublezero daemon: {}", res.status());
                }
                let data = res
                    .into_body()
                    .collect()
                    .await
                    .map_err(|e| eyre!("Unable to read response body: {e}"))?
                    .to_bytes();
                if data.is_empty() {
                    eyre::bail!("No data returned from daemon /status");
                }
                parse_daemon_response::<Vec<StatusResponse>>(&data, "/status")
            }
            Err(e) => Err(eyre!("Unable to connect to doublezero daemon: {e}")),
        }
    }

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
        if res.status() != 200 {
            eyre::bail!("Unable to connect to doublezero daemon: {}", res.status());
        }
        let data = res
            .into_body()
            .collect()
            .await
            .map_err(|e| eyre!("Unable to read response body: {e}"))?
            .to_bytes();
        parse_daemon_response::<V2StatusResponse>(&data, "/v2/status")
    }

    async fn enable(&self) -> eyre::Result<()> {
        let client: Client<UnixConnector, Full<Bytes>> =
            Client::builder(TokioExecutor::new()).build(UnixConnector);
        let req = Request::builder()
            .method(Method::POST)
            .uri(Uri::new(&self.socket_path, "/enable"))
            .body(Full::from(Bytes::new()))?;
        let res = client
            .request(req)
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {e}"))?;
        if res.status() != 200 {
            eyre::bail!("Failed to enable reconciler: {}", res.status());
        }
        Ok(())
    }

    async fn disable(&self) -> eyre::Result<()> {
        let client: Client<UnixConnector, Full<Bytes>> =
            Client::builder(TokioExecutor::new()).build(UnixConnector);
        let req = Request::builder()
            .method(Method::POST)
            .uri(Uri::new(&self.socket_path, "/disable"))
            .body(Full::from(Bytes::new()))?;
        let res = client
            .request(req)
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {e}"))?;
        if res.status() != 200 {
            eyre::bail!("Failed to disable reconciler: {}", res.status());
        }
        Ok(())
    }

    async fn routes(&self) -> eyre::Result<Vec<RouteRecord>> {
        let client = Client::builder(TokioExecutor::new()).build(UnixConnector);
        let req = Request::builder()
            .method(Method::GET)
            .uri(Uri::new(&self.socket_path, "/routes"))
            .body(Empty::<Bytes>::new())?;
        let res = client
            .request(req)
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {e}"))?;
        if res.status() != 200 {
            eyre::bail!("Unable to connect to doublezero daemon: {}", res.status());
        }
        let data = res
            .into_body()
            .collect()
            .await
            .map_err(|e| eyre!("Unable to read response body: {e}"))?
            .to_bytes();
        parse_daemon_response::<Vec<RouteRecord>>(&data, "/routes")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_latency_record_serde_roundtrip() {
        let record = LatencyRecord {
            device_pk: "DevicePubkey123".to_string(),
            device_code: "device1".to_string(),
            device_ip: "5.6.7.8".to_string(),
            min_latency_ns: 1000000,
            max_latency_ns: 5000000,
            avg_latency_ns: 3000000,
            reachable: true,
        };
        let json = serde_json::to_string(&record).unwrap();
        let deserialized: LatencyRecord = serde_json::from_str(&json).unwrap();
        assert_eq!(deserialized.device_pk, "DevicePubkey123");
        assert_eq!(deserialized.min_latency_ns, 1000000);
        assert!(deserialized.reachable);
    }

    #[test]
    fn test_route_record_serde_roundtrip() {
        let route = RouteRecord {
            network: "10.0.0.0/24".to_string(),
            local_ip: "10.1.2.3".to_string(),
            peer_ip: "10.1.2.4".to_string(),
            kernel_state: "active".to_string(),
            liveness_last_updated: Some("2024-01-15T12:00:00Z".to_string()),
            liveness_state: Some("up".to_string()),
            liveness_state_reason: Some("healthy".to_string()),
            peer_client_version: Some("0.8.6".to_string()),
        };
        let json = serde_json::to_string(&route).unwrap();
        let deserialized: RouteRecord = serde_json::from_str(&json).unwrap();
        assert_eq!(deserialized.network, "10.0.0.0/24");
        assert_eq!(deserialized.peer_client_version.as_deref(), Some("0.8.6"));
    }

    #[test]
    fn test_route_record_serde_with_nulls() {
        let route = RouteRecord {
            network: "10.0.0.0/24".to_string(),
            local_ip: "10.1.2.3".to_string(),
            peer_ip: "10.1.2.4".to_string(),
            kernel_state: "active".to_string(),
            liveness_last_updated: None,
            liveness_state: None,
            liveness_state_reason: None,
            peer_client_version: None,
        };
        let json = serde_json::to_value(&route).unwrap();
        assert!(json.get("liveness_state").unwrap().is_null());
    }

    #[test]
    fn test_status_response_serde_roundtrip() {
        let status = StatusResponse {
            doublezero_status: DoubleZeroStatus {
                session_status: "BGP Session Up".to_string(),
                last_session_update: Some(1625247600),
            },
            tunnel_name: Some("doublezero1".to_string()),
            tunnel_src: Some("10.0.0.1".to_string()),
            tunnel_dst: Some("5.6.7.8".to_string()),
            doublezero_ip: Some("10.1.2.3".to_string()),
            user_type: Some("IBRL".to_string()),
        };
        let json = serde_json::to_string(&status).unwrap();
        let deserialized: StatusResponse = serde_json::from_str(&json).unwrap();
        assert_eq!(
            deserialized.doublezero_status.session_status,
            "BGP Session Up"
        );
    }

    #[test]
    fn test_v2_status_response_serde() {
        let json = r#"{
            "reconciler_enabled": true,
            "client_ip": "1.2.3.4",
            "network": "mainnet",
            "services": []
        }"#;
        let resp: V2StatusResponse = serde_json::from_str(json).unwrap();
        assert!(resp.reconciler_enabled);
        assert_eq!(resp.client_ip, "1.2.3.4");
        assert!(resp.services.is_empty());
    }

    #[test]
    fn test_multicast_groups_defaults() {
        let json = "{}";
        let groups: MulticastGroups = serde_json::from_str(json).unwrap();
        assert!(groups.publisher.is_empty());
        assert!(groups.subscriber.is_empty());
    }

    #[test]
    fn test_v2_service_status_serde_missing_multicast_groups() {
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

    #[test]
    fn test_v2_service_status_serde_populated_multicast_groups() {
        let json = r#"{
            "doublezero_status": {"session_status": "BGP Session Up", "last_session_update": null},
            "tunnel_name": "doublezero1", "tunnel_src": "10.0.0.1", "tunnel_dst": "5.6.7.8",
            "doublezero_ip": null, "user_type": "Multicast",
            "current_device": "dz1", "lowest_latency_device": "dz1",
            "metro": "ams", "tenant": "acme",
            "multicast_groups": {
                "publisher": ["solana-lv"],
                "subscriber": ["solana-ams", "solana-fra"]
            }
        }"#;
        let svc: V2ServiceStatus = serde_json::from_str(json).unwrap();
        assert_eq!(svc.multicast_groups.publisher, vec!["solana-lv"]);
        assert_eq!(
            svc.multicast_groups.subscriber,
            vec!["solana-ams", "solana-fra"]
        );
    }

    #[test]
    fn test_v2_service_status_serde_empty_multicast_arrays() {
        let json = r#"{
            "doublezero_status": {"session_status": "BGP Session Up", "last_session_update": null},
            "tunnel_name": null, "tunnel_src": null, "tunnel_dst": null,
            "doublezero_ip": "10.0.0.1", "user_type": "IBRL",
            "current_device": "dz1", "lowest_latency_device": "dz1",
            "metro": "ams", "tenant": "",
            "multicast_groups": {"publisher": [], "subscriber": []}
        }"#;
        let svc: V2ServiceStatus = serde_json::from_str(json).unwrap();
        assert!(svc.multicast_groups.publisher.is_empty());
        assert!(svc.multicast_groups.subscriber.is_empty());
    }

    #[test]
    fn test_daemon_client_impl_uses_explicit_socket_path() {
        let socket_path =
            std::env::temp_dir().join(format!("doublezerod-test-{}.sock", std::process::id()));
        {
            let mut file = File::create(&socket_path).expect("create socket placeholder");
            std::io::Write::write_all(&mut file, b"test").expect("write");
        }
        let client = DaemonClientImpl::new(Some(socket_path.to_string_lossy().into_owned()));
        assert!(client.daemon_check());
        assert!(client.daemon_can_open());
        std::fs::remove_file(socket_path).expect("cleanup");
    }
}
