use chrono::DateTime;
use eyre::eyre;
use http_body_util::{BodyExt, Empty, Full};
use hyper::{body::Bytes, Method, Request};
use hyper_util::{client::legacy::Client, rt::TokioExecutor};
use hyperlocal::{UnixConnector, Uri};
use mockall::automock;
use serde::{Deserialize, Serialize};
use std::fmt;
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
    #[tabled(rename = "pubkey")]
    pub device_pk: String,
    #[tabled(rename = "code")]
    pub device_code: String,
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

#[derive(Tabled, Serialize, Deserialize, Debug)]
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

#[derive(Tabled, Serialize, Deserialize, Debug)]
pub struct DoubleZeroStatus {
    #[tabled(rename = "Tunnel status")]
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

#[derive(Serialize, Debug, Clone)]
pub struct UpdateConfigRequest {
    pub ledger_rpc_url: String,
    pub serviceability_program_id: String, // base58 pubkey string
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
    async fn update_config(&self, args: UpdateConfigRequest) -> eyre::Result<()>;
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
        std::path::Path::new(&self.socket_path).exists()
    }

    fn service_controller_can_open(&self) -> bool {
        match std::fs::File::options()
            .read(true)
            .write(true)
            .open(&self.socket_path)
        {
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

    async fn update_config(&self, args: UpdateConfigRequest) -> eyre::Result<()> {
        let client = Client::builder(TokioExecutor::new()).build(UnixConnector);

        let body_bytes =
            serde_json::to_vec(&args).map_err(|e| eyre!("Unable to serialize request: {e}"))?;

        let req = Request::builder()
            .method(Method::PUT) // choose PUT for idempotent config updates
            .uri(Uri::new(&self.socket_path, "/config"))
            .header(hyper::header::CONTENT_TYPE, "application/json")
            .body(Full::new(Bytes::from(body_bytes)))?;

        let res = client
            .request(req)
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {e}"))?;

        let status = res.status();
        let data = res
            .into_body()
            .collect()
            .await
            .map_err(|e| eyre!("Unable to read response body: {e}"))?
            .to_bytes();

        if status != hyper::StatusCode::OK {
            if let Ok(err) = serde_json::from_slice::<ErrorResponse>(&data) {
                eyre::bail!(err.description);
            }
            eyre::bail!("Config update failed: HTTP {}", status);
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use eyre::Result;
    use http::HeaderMap;
    use http_body_util::{BodyExt, Full};
    use hyper::{body::Incoming, server::conn::http1, service::service_fn, Response, StatusCode};
    use serde_json::json;
    use std::sync::{
        atomic::{AtomicUsize, Ordering},
        Arc,
    };
    use tempfile::TempDir;
    use tokio::{
        net::UnixListener,
        task::JoinHandle,
        time::{sleep, Duration},
    };

    #[tokio::test]
    async fn update_config_success_ok() -> Result<()> {
        let server = spawn_unix_server(|req| async move {
            let (method, path, body, headers) = req_bytes_to_vec(req).await;
            assert_eq!(method, Method::PUT);
            assert_eq!(path, "/config");
            assert_eq!(
                headers.get(hyper::header::CONTENT_TYPE).unwrap(),
                "application/json"
            );

            let v: serde_json::Value = serde_json::from_slice(&body).unwrap();
            assert_eq!(v["ledger_rpc_url"], "http://ledger2");
            assert_eq!(v["serviceability_program_id"], "SomeBase58Key");

            let resp = json!({"status":"ok"});
            Ok(Response::builder()
                .status(StatusCode::OK)
                .body(Full::new(Bytes::from(resp.to_string())))
                .unwrap())
        })
        .await;

        let svc = ServiceControllerImpl {
            socket_path: server.path.clone(),
        };
        svc.update_config(UpdateConfigRequest {
            ledger_rpc_url: "http://ledger2".into(),
            serviceability_program_id: "SomeBase58Key".into(),
        })
        .await?;
        Ok(())
    }

    #[tokio::test]
    async fn update_config_json_error_payload() -> Result<()> {
        let server = spawn_unix_server(|_req| async move {
            let err = json!({"status":"error","description":"bad key"});
            Ok(Response::builder()
                .status(StatusCode::BAD_REQUEST)
                .body(Full::new(Bytes::from(err.to_string())))
                .unwrap())
        })
        .await;

        let svc = ServiceControllerImpl {
            socket_path: server.path.clone(),
        };
        let err = svc
            .update_config(UpdateConfigRequest {
                ledger_rpc_url: "http://x".into(),
                serviceability_program_id: "bad".into(),
            })
            .await
            .unwrap_err();
        assert!(format!("{err}").contains("bad key"));
        Ok(())
    }

    #[tokio::test]
    async fn update_config_http_error_non_json() -> Result<()> {
        let server = spawn_unix_server(|_req| async move {
            Ok(Response::builder()
                .status(StatusCode::INTERNAL_SERVER_ERROR)
                .body(Full::new(Bytes::from_static(b"oops")))
                .unwrap())
        })
        .await;

        let svc = ServiceControllerImpl {
            socket_path: server.path.clone(),
        };
        let err = svc
            .update_config(UpdateConfigRequest {
                ledger_rpc_url: "http://x".into(),
                serviceability_program_id: "key".into(),
            })
            .await
            .unwrap_err();
        assert!(format!("{err}").contains("Config update failed: HTTP 500"));
        Ok(())
    }

    #[tokio::test]
    async fn update_config_sends_once_even_with_many_concurrent_calls() -> Result<()> {
        // optional: demonstrates independent client calls; also asserts each request is PUT /config
        let counter = Arc::new(AtomicUsize::new(0));
        let ctr = counter.clone();
        let server = spawn_unix_server(move |req| {
            let ctr = ctr.clone();
            async move {
                ctr.fetch_add(1, Ordering::SeqCst);
                let (method, path, ..) = req_bytes_to_vec(req).await;
                assert_eq!(method, Method::PUT);
                assert_eq!(path, "/config");
                let resp = json!({"status":"ok"});
                Ok(Response::builder()
                    .status(StatusCode::OK)
                    .body(Full::new(Bytes::from(resp.to_string())))
                    .unwrap())
            }
        })
        .await;

        let svc = ServiceControllerImpl {
            socket_path: server.path.clone(),
        };
        let req = UpdateConfigRequest {
            ledger_rpc_url: "http://ledger2".into(),
            serviceability_program_id: "SomeBase58Key".into(),
        };

        // fire a few in parallel to ensure no panics and correct method/path each time
        let futs = (0..5).map(|_| svc.update_config(req.clone()));
        let results = futures::future::join_all(futs).await;
        for r in results {
            r.unwrap();
        }
        assert_eq!(counter.load(Ordering::SeqCst), 5);
        Ok(())
    }

    struct TestServer {
        _jh: JoinHandle<()>,
        path: String,
        _dir: TempDir, // keep-alive
    }

    async fn spawn_unix_server<F, Fut>(respond: F) -> TestServer
    where
        F: Fn(hyper::Request<Incoming>) -> Fut + Send + Sync + 'static + Clone,
        Fut: std::future::Future<Output = hyper::Result<Response<Full<Bytes>>>> + Send + 'static,
    {
        let dir = tempfile::tempdir().unwrap();
        let sock_path = dir.path().join("dz.sock");
        let listener = UnixListener::bind(&sock_path).unwrap();
        let path_str = sock_path.to_string_lossy().to_string();

        let svc = Arc::new(respond);
        let jh = tokio::spawn(async move {
            loop {
                let (stream, _) = match listener.accept().await {
                    Ok(s) => s,
                    Err(_) => break,
                };
                let svc2 = svc.clone();
                tokio::spawn(async move {
                    let io = hyper_util::rt::TokioIo::new(stream);
                    let service = service_fn(move |req| (svc2)(req));
                    let _ = http1::Builder::new().serve_connection(io, service).await;
                });
            }
        });

        // tiny delay to ensure listener is ready
        sleep(Duration::from_millis(10)).await;

        TestServer {
            _jh: jh,
            path: path_str,
            _dir: dir,
        }
    }

    async fn req_bytes_to_vec(
        req: hyper::Request<Incoming>,
    ) -> (Method, String, Vec<u8>, HeaderMap) {
        let method: Method = req.method().clone();
        let path: String = req.uri().path().to_string();
        let headers: HeaderMap = req.headers().clone();
        let body: Vec<u8> = req.into_body().collect().await.unwrap().to_bytes().to_vec();
        (method, path, body, headers)
    }
}
