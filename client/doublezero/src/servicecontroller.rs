use chrono::DateTime;
use doublezero_config::Environment;
use eyre::eyre;
use http::StatusCode;
use http_body_util::{BodyExt, Empty, Full};
use hyper::{body::Bytes, Method, Request};
use hyper_util::{client::legacy::Client, rt::TokioExecutor};
use hyperlocal::{UnixConnector, Uri};
use mockall::automock;
use serde::{Deserialize, Serialize};
use std::{fmt, fs::File, net::Ipv4Addr, path::Path};
use tabled::{derive::display, Tabled};

const NANOS_TO_MS: f32 = 1000000.0;

#[derive(Serialize, Debug, PartialEq)]
pub struct ProvisioningRequest {
    pub tunnel_src: String,
    pub tunnel_dst: String,
    pub tunnel_net: String,
    pub doublezero_ip: String,
    pub doublezero_prefixes: Vec<String>,
    pub bgp_local_asn: Option<u32>,
    pub bgp_remote_asn: Option<u32>,
    pub user_type: String,
    pub mcast_pub_groups: Option<Vec<String>>,
    pub mcast_sub_groups: Option<Vec<String>>,
}

#[derive(Deserialize, Debug)]
pub struct ProvisioningResponse {
    pub status: String,
    pub description: Option<String>,
}

#[derive(Clone, Tabled, Deserialize, Serialize, Debug)]
pub struct LatencyRecord {
    #[tabled(rename = "Pubkey")]
    pub device_pk: String,
    #[tabled(rename = "Code")]
    pub device_code: String,
    #[tabled(rename = "IP")]
    pub device_ip: String,
    #[tabled(display = "display_as_ms", rename = "Min")]
    pub min_latency_ns: i32,
    #[tabled(display = "display_as_ms", rename = "Max")]
    pub max_latency_ns: i32,
    #[tabled(display = "display_as_ms", rename = "Avg")]
    pub avg_latency_ns: i32,
    pub reachable: bool,
}

fn display_as_ms(latency: &i32) -> String {
    format!("{:.2}ms", (*latency as f32 / NANOS_TO_MS))
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

#[derive(Serialize, Debug)]
pub struct RemoveTunnelCliCommand {
    pub user_type: String,
}

#[derive(Deserialize, Debug)]
pub struct RemoveResponse {
    pub status: String,
    pub description: Option<String>,
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

#[derive(Serialize, Deserialize, Debug)]
pub struct ResolveRouteRequest {
    pub dst: Ipv4Addr,
}
impl fmt::Display for ResolveRouteRequest {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "dst: {}", self.dst)
    }
}

#[derive(Deserialize, Debug)]
pub struct ResolveRouteResponse {
    pub src: Option<Ipv4Addr>,
}

impl fmt::Display for ResolveRouteResponse {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "src: {:?}", self.src)
    }
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

#[derive(Deserialize, Debug)]
pub struct ErrorResponse {
    pub status: String,
    pub description: String,
}

#[automock]
pub trait ServiceController {
    fn service_controller_check(&self) -> bool;
    fn service_controller_can_open(&self) -> bool;
    async fn get_config(&self) -> eyre::Result<GetConfigResponse>;
    async fn get_env(&self) -> eyre::Result<Environment>;
    async fn latency(&self) -> eyre::Result<Vec<LatencyRecord>>;
    async fn provisioning(&self, args: ProvisioningRequest) -> eyre::Result<ProvisioningResponse>;
    async fn remove(&self, args: RemoveTunnelCliCommand) -> eyre::Result<RemoveResponse>;
    async fn status(&self) -> eyre::Result<Vec<StatusResponse>>;
    async fn routes(&self) -> eyre::Result<Vec<RouteRecord>>;
    async fn resolve_route(&self, args: ResolveRouteRequest) -> eyre::Result<ResolveRouteResponse>;
}

pub struct ServiceControllerImpl {
    pub socket_path: String,
}

impl ServiceControllerImpl {
    pub fn new(socket_path: Option<String>) -> ServiceControllerImpl {
        ServiceControllerImpl {
            socket_path: socket_path.unwrap_or("/var/run/doublezerod/doublezerod.sock".to_string()),
        }
    }
}

impl ServiceController for ServiceControllerImpl {
    fn service_controller_check(&self) -> bool {
        Path::new("/var/run/doublezerod/doublezerod.sock").exists()
    }

    fn service_controller_can_open(&self) -> bool {
        let file = File::options()
            .read(true)
            .write(true)
            .open("/var/run/doublezerod/doublezerod.sock");
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

        match serde_json::from_slice::<GetConfigResponse>(&data) {
            Ok(response) => Ok(response),
            Err(e) => match serde_json::from_slice::<ErrorResponse>(&data) {
                Ok(response) => {
                    if response.status == "error" {
                        Err(eyre!(response.description))
                    } else {
                        Err(eyre!("Unable to parse LatencyRecord: {e}"))
                    }
                }
                Err(e) => Err(eyre!("Unable to parse ErrorResponse: {e}")),
            },
        }
    }

    async fn get_env(&self) -> eyre::Result<Environment> {
        let config = self.get_config().await?;
        Ok(Environment::from_program_id(&config.program_id).unwrap_or_default())
    }

    async fn latency(&self) -> eyre::Result<Vec<LatencyRecord>> {
        let uri = Uri::new(&self.socket_path, "/latency").into();
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

        match serde_json::from_slice::<Vec<LatencyRecord>>(&data) {
            Ok(response) => Ok(response),
            Err(e) => match serde_json::from_slice::<ErrorResponse>(&data) {
                Ok(response) => {
                    if response.status == "error" {
                        Err(eyre!(response.description))
                    } else {
                        Err(eyre!("Unable to parse LatencyRecord: {e}"))
                    }
                }
                Err(e) => Err(eyre!("Unable to parse ErrorResponse: {e}")),
            },
        }
    }

    async fn provisioning(&self, args: ProvisioningRequest) -> eyre::Result<ProvisioningResponse> {
        let client = Client::builder(TokioExecutor::new()).build(UnixConnector);
        let body_bytes =
            serde_json::to_vec(&args).map_err(|e| eyre!("Unable to serialize request: {e}"))?;

        let req = Request::builder()
            .method(Method::POST)
            .uri(Uri::new(&self.socket_path, "/provision"))
            .body(Full::new(Bytes::from(body_bytes)))?;

        let res = client.request(req).await?;
        let data = res
            .into_body()
            .collect()
            .await
            .map_err(|e| eyre!("Unable to read response body: {e}"))?
            .to_bytes();

        let response = serde_json::from_slice::<ProvisioningResponse>(&data)?;
        if response.status == "error" {
            Err(eyre!(response.description.unwrap_or_default()))
        } else {
            Ok(response)
        }
    }

    async fn remove(&self, args: RemoveTunnelCliCommand) -> eyre::Result<RemoveResponse> {
        let client = Client::builder(TokioExecutor::new()).build(UnixConnector);
        let body_bytes =
            serde_json::to_vec(&args).map_err(|e| eyre!("Unable to serialize request: {e}"))?;

        let req = Request::builder()
            .method(Method::POST)
            .uri(Uri::new(&self.socket_path, "/remove"))
            .body(Full::new(Bytes::from(body_bytes)))?;

        let res = client.request(req).await?;
        let data = res
            .into_body()
            .collect()
            .await
            .map_err(|e| eyre!("Unable to read response body: {e}"))?
            .to_bytes();

        let response = serde_json::from_slice::<RemoveResponse>(&data)?;
        if response.status == "error" {
            Err(eyre!(response.description.unwrap_or_default()))
        } else {
            Ok(response)
        }
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

                match serde_json::from_slice::<Vec<StatusResponse>>(&data) {
                    Ok(response) => Ok(response),
                    Err(e) => {
                        println!("Data: {data:?}");

                        if data.is_empty() {
                            eyre::bail!("No data returned");
                        }

                        match serde_json::from_slice::<ErrorResponse>(&data) {
                            Ok(response) => {
                                if response.status == "error" {
                                    Err(eyre!(response.description))
                                } else {
                                    Err(eyre!("Unable to parse StatusResponse: {e}"))
                                }
                            }
                            Err(e) => Err(eyre!("Unable to parse ErrorResponse: {e}")),
                        }
                    }
                }
            }
            Err(e) => Err(eyre!("Unable to connect to doublezero daemon: {e}")),
        }
    }

    async fn routes(&self) -> eyre::Result<Vec<RouteRecord>> {
        let client = Client::builder(TokioExecutor::new()).build(UnixConnector);
        let req = Request::builder()
            .method(Method::GET)
            .uri(Uri::new(&self.socket_path, "/routes"))
            .body(Empty::<Bytes>::new())?;
        let res = client.request(req).await?;
        let data = res.into_body().collect().await?.to_bytes();
        let response = serde_json::from_slice::<Vec<RouteRecord>>(&data)?;
        Ok(response)
    }

    async fn resolve_route(&self, args: ResolveRouteRequest) -> eyre::Result<ResolveRouteResponse> {
        let client = Client::builder(TokioExecutor::new()).build(UnixConnector);
        let body_bytes =
            serde_json::to_vec(&args).map_err(|e| eyre!("Unable to serialize request: {e}"))?;
        let req = Request::builder()
            .method(Method::POST)
            .uri(Uri::new(&self.socket_path, "/resolve-route"))
            .body(Full::new(Bytes::from(body_bytes)))?;
        let res = client.request(req).await?;

        // If route not found (404) or API error, return src=None instead of error
        if res.status() == StatusCode::NOT_FOUND || !res.status().is_success() {
            return Ok(ResolveRouteResponse { src: None });
        }

        let data = res.into_body().collect().await?.to_bytes();
        let response = serde_json::from_slice::<ResolveRouteResponse>(&data)?;
        Ok(response)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Test that validates the JSON output format for LatencyRecord.
    /// This test catches breaking changes to the JSON API contract.
    #[test]
    fn test_latency_record_json_output_format() {
        let latency = LatencyRecord {
            device_pk: "DevicePubkey123".to_string(),
            device_code: "device1".to_string(),
            device_ip: "5.6.7.8".to_string(),
            min_latency_ns: 1000000,
            max_latency_ns: 5000000,
            avg_latency_ns: 3000000,
            reachable: true,
        };

        let json_output = serde_json::to_value(&latency).expect("Failed to serialize");

        // Validate all fields are present
        assert!(
            json_output.get("device_pk").is_some(),
            "Missing 'device_pk' field"
        );
        assert!(
            json_output.get("device_code").is_some(),
            "Missing 'device_code' field"
        );
        assert!(
            json_output.get("device_ip").is_some(),
            "Missing 'device_ip' field"
        );
        assert!(
            json_output.get("min_latency_ns").is_some(),
            "Missing 'min_latency_ns' field"
        );
        assert!(
            json_output.get("max_latency_ns").is_some(),
            "Missing 'max_latency_ns' field"
        );
        assert!(
            json_output.get("avg_latency_ns").is_some(),
            "Missing 'avg_latency_ns' field"
        );
        assert!(
            json_output.get("reachable").is_some(),
            "Missing 'reachable' field"
        );

        // Validate field values
        assert_eq!(json_output.get("device_pk").unwrap(), "DevicePubkey123");
        assert_eq!(json_output.get("device_code").unwrap(), "device1");
        assert_eq!(json_output.get("device_ip").unwrap(), "5.6.7.8");
        assert_eq!(json_output.get("min_latency_ns").unwrap(), 1000000);
        assert_eq!(json_output.get("max_latency_ns").unwrap(), 5000000);
        assert_eq!(json_output.get("avg_latency_ns").unwrap(), 3000000);
        assert_eq!(json_output.get("reachable").unwrap(), true);
    }

    /// Test that validates the JSON output format for RouteRecord.
    /// This test catches breaking changes to the JSON API contract.
    #[test]
    fn test_route_record_json_output_format() {
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

        let json_output = serde_json::to_value(&route).expect("Failed to serialize");

        // Validate all fields are present
        assert!(
            json_output.get("network").is_some(),
            "Missing 'network' field"
        );
        assert!(
            json_output.get("local_ip").is_some(),
            "Missing 'local_ip' field"
        );
        assert!(
            json_output.get("peer_ip").is_some(),
            "Missing 'peer_ip' field"
        );
        assert!(
            json_output.get("kernel_state").is_some(),
            "Missing 'kernel_state' field"
        );
        assert!(
            json_output.get("liveness_last_updated").is_some(),
            "Missing 'liveness_last_updated' field"
        );
        assert!(
            json_output.get("liveness_state").is_some(),
            "Missing 'liveness_state' field"
        );
        assert!(
            json_output.get("liveness_state_reason").is_some(),
            "Missing 'liveness_state_reason' field"
        );
        assert!(
            json_output.get("peer_client_version").is_some(),
            "Missing 'peer_client_version' field"
        );

        // Validate field values
        assert_eq!(json_output.get("network").unwrap(), "10.0.0.0/24");
        assert_eq!(json_output.get("local_ip").unwrap(), "10.1.2.3");
        assert_eq!(json_output.get("peer_ip").unwrap(), "10.1.2.4");
        assert_eq!(json_output.get("kernel_state").unwrap(), "active");
        assert_eq!(
            json_output.get("liveness_last_updated").unwrap(),
            "2024-01-15T12:00:00Z"
        );
        assert_eq!(json_output.get("liveness_state").unwrap(), "up");
        assert_eq!(json_output.get("liveness_state_reason").unwrap(), "healthy");
        assert_eq!(json_output.get("peer_client_version").unwrap(), "0.8.6");
    }

    /// Test RouteRecord JSON output with null optional fields
    #[test]
    fn test_route_record_json_output_format_with_nulls() {
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

        let json_output = serde_json::to_value(&route).expect("Failed to serialize");

        // Validate optional fields are null
        assert!(
            json_output.get("liveness_last_updated").unwrap().is_null(),
            "liveness_last_updated should be null"
        );
        assert!(
            json_output.get("liveness_state").unwrap().is_null(),
            "liveness_state should be null"
        );
        assert!(
            json_output.get("liveness_state_reason").unwrap().is_null(),
            "liveness_state_reason should be null"
        );
        assert!(
            json_output.get("peer_client_version").unwrap().is_null(),
            "peer_client_version should be null"
        );

        // Required fields should still be present
        assert_eq!(json_output.get("network").unwrap(), "10.0.0.0/24");
        assert_eq!(json_output.get("local_ip").unwrap(), "10.1.2.3");
    }

    /// Test StatusResponse JSON output format
    #[test]
    fn test_status_response_json_output_format() {
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
        assert_eq!(dz_status.get("last_session_update").unwrap(), 1625247600);
    }
}
