use chrono::DateTime;
use eyre::eyre;
use http_body_util::{BodyExt, Empty, Full};
use hyper::{body::Bytes, Method, Request};
use hyper_util::{client::legacy::Client, rt::TokioExecutor};
use hyperlocal::{UnixConnector, Uri};
use mockall::automock;
use serde::{Deserialize, Serialize};
use std::{fmt, fs::File, path::Path};
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

#[derive(Clone, Tabled, Deserialize, Debug)]
pub struct LatencyRecord {
    #[tabled(rename = "pubkey")]
    pub device_pk: String,
    #[tabled(rename = "ip")]
    pub device_ip: String,
    #[tabled(display = "display_as_ms", rename = "min")]
    pub min_latency_ns: i32,
    #[tabled(display = "display_as_ms", rename = "max")]
    pub max_latency_ns: i32,
    #[tabled(display = "display_as_ms", rename = "avg")]
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
            "device: {}, ip: {}, latency min: {}, max: {}, avg: {}, reachable: {}",
            self.device_pk,
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

#[derive(Tabled, Deserialize, Debug)]
#[tabled(display(Option, "display::option", ""))]
pub struct StatusResponse {
    #[tabled(inline)]
    pub doublezero_status: DoubleZeroStatus,
    #[tabled(rename = "Tunnel Name")]
    pub tunnel_name: Option<String>,
    #[tabled(rename = "Tunnel src")]
    pub tunnel_src: Option<String>,
    #[tabled(rename = "Tunnel dst")]
    pub tunnel_dst: Option<String>,
    #[tabled(rename = "Doublezero IP")]
    pub doublezero_ip: Option<String>,
    #[tabled(rename = "User Type")]
    pub user_type: Option<String>,
}

#[derive(Tabled, Deserialize, Debug)]
pub struct DoubleZeroStatus {
    #[tabled(rename = "Tunnel status")]
    pub session_status: String,
    #[tabled(rename = "Last Session Update", display = "maybe_i64_to_dt_str")]
    pub last_session_update: Option<i64>,
}

fn maybe_i64_to_dt_str(maybe_i64_dt: &Option<i64>) -> String {
    let dt_i64 = maybe_i64_dt.unwrap_or_default();
    if dt_i64 == 0 {
        "no session data".to_string()
    } else {
        DateTime::from_timestamp(dt_i64, 0)
            .expect("invalid timestamp")
            .to_string()
    }
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
    async fn latency(&self) -> eyre::Result<Vec<LatencyRecord>>;
    async fn provisioning(&self, args: ProvisioningRequest) -> eyre::Result<ProvisioningResponse>;
    async fn remove(&self, args: RemoveTunnelCliCommand) -> eyre::Result<RemoveResponse>;
    async fn status(&self) -> eyre::Result<Vec<StatusResponse>>;
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
}
