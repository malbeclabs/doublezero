use eyre::eyre;
use hyper::body::to_bytes;
use hyper::{Body, Client, Method, Request};
use hyperlocal::{UnixConnector, Uri};
use serde::{Deserialize, Serialize};
use std::fmt;
use std::fs::File;
use std::path::Path;

#[derive(Serialize, Debug)]
pub struct ProvisioningRequest {
    pub tunnel_src: String,
    pub tunnel_dst: String,
    pub tunnel_net: String,
    pub doublezero_ip: String,
    pub doublezero_prefixes: Vec<String>,
    pub bgp_local_asn: Option<u32>,
    pub bgp_remote_asn: Option<u32>,
}

#[derive(Deserialize, Debug)]
pub struct ProvisioningResponse {
    pub status: String,
    pub description: Option<String>,
}

#[derive(Deserialize, Debug)]
pub struct LatencyRecord {
    pub device_pk: String,
    pub device_ip: String,
    pub min_latency_ns: i32,
    pub max_latency_ns: i32,
    pub avg_latency_ns: i32,
    pub reachable: bool,
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
pub struct RemoveTunnelArgs {}

#[derive(Deserialize, Debug)]
pub struct RemoveResponse {
    pub status: String,
    pub description: Option<String>,
}

#[derive(Deserialize, Debug)]
pub struct StatusResponse {
    pub status: String,
    pub tunnel_name: Option<String>,
    pub tunnel_src: Option<String>,
    pub tunnel_dst: Option<String>,
    pub doublezero_ip: Option<String>,
}

#[derive(Deserialize, Debug)]
pub struct ErrorResponse {
    pub status: String,
    pub description: String,
}


pub fn service_controller_check() -> bool {
    Path::new("/var/run/doublezerod/doublezerod.sock").exists()
}

pub fn service_controller_can_open() -> bool {
    let file = File::options()
        .read(true)
        .write(true)
        .open("/var/run/doublezerod/doublezerod.sock");
    match file {
        Ok(_) => true,
        Err(e) => !matches!(e.kind(), std::io::ErrorKind::PermissionDenied)
    }
}

pub struct ServiceController {
    pub socket_path: String,
}

impl ServiceController {
    pub fn new(socket_path: Option<String>) -> ServiceController {
        ServiceController {
            socket_path: socket_path.unwrap_or("/var/run/doublezerod/doublezerod.sock".to_string()),
        }
    }

    pub async fn latency(&self) -> eyre::Result<Vec<LatencyRecord>> {
        let uri: Uri = Uri::new(&self.socket_path, "/latency");
        let client: Client<UnixConnector, Body> = Client::builder().build(UnixConnector);
        let res = client
            .get(uri.into())
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {}", e))?;

        let data = to_bytes(res.into_body()).await?;
        match serde_json::from_slice::<Vec<LatencyRecord>>(&data) {
            Ok(respose) => Ok(respose),
            Err(e) => match serde_json::from_slice::<ErrorResponse>(&data) {
                Ok(respose) => {
                    if respose.status == "error" {
                        Err(eyre!(respose.description))
                    } else {
                        Err(eyre!("Unable to parse response: {}", e))
                    }
                }
                Err(_) => Err(eyre!("Unable to parse response: {}", e)),
            },
        }
    }

    pub async fn provisioning(
        &self,
        args: ProvisioningRequest,
    ) -> eyre::Result<ProvisioningResponse> {
        let client: Client<UnixConnector, Body> = Client::builder().build(UnixConnector);

        let req = Request::builder()
            .method(Method::POST)
            .uri(Uri::new(&self.socket_path, "/provision"))
            .body(Body::from(
                serde_json::to_vec(&args)
                    .map_err(|e| eyre!("Unable to serialize request: {}", e))?,
            ))?;
        let res = client.request(req).await?;
        let data = to_bytes(res.into_body())
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {}", e))?;

        let respose = serde_json::from_slice::<ProvisioningResponse>(&data)?;
        if respose.status == "error" {
            Err(eyre!(respose.description.unwrap_or_default()))
        } else {
            Ok(respose)
        }
    }

    pub async fn remove(&self, args: RemoveTunnelArgs) -> eyre::Result<RemoveResponse> {
        let client: Client<UnixConnector, Body> = Client::builder().build(UnixConnector);

        let req = Request::builder()
            .method(Method::POST)
            .uri(Uri::new(&self.socket_path, "/remove"))
            .body(Body::from(
                serde_json::to_vec(&args)
                    .map_err(|e| eyre!("Unable to serialize request: {}", e))?,
            ))?;
        let res = client.request(req).await?;
        let data = to_bytes(res.into_body())
            .await
            .map_err(|e| eyre!("Unable to connect to doublezero daemon: {}", e))?;

        let respose = serde_json::from_slice::<RemoveResponse>(&data)?;
        if respose.status == "error" {
            Err(eyre!(respose.description.unwrap_or_default()))
        } else {
            Ok(respose)
        }
    }

    pub async fn status(&self) -> eyre::Result<StatusResponse> {
        let client: Client<UnixConnector, Body> = Client::builder().build(UnixConnector);

        let req = Request::builder()
            .method(Method::GET)
            .uri(Uri::new(&self.socket_path, "/status"))
            .body(Body::empty())?;

        match client.request(req).await {
            Ok(res) => {

                if res.status() != 200 {
                    return Err(eyre!("Unable to connect to doublezero daemon: {}", res.status()));
                }

                let data = to_bytes(res.into_body())
                    .await
                    .map_err(|e| eyre!("Unable to connect to doublezero daemon: {}", e))?;

                match serde_json::from_slice::<StatusResponse>(&data) {
                    Ok(respose) => Ok(respose),
                    Err(e) => {
                        println!("Data: {:?}", data);

                        if data.is_empty() {
                            return Err(eyre!("No data returned"));
                        }

                        match serde_json::from_slice::<ErrorResponse>(&data) {
                            Ok(respose) => {
                                if respose.status == "error" {
                                    Err(eyre!(respose.description))
                                } else {
                                    Err(eyre!("Unable to parse response: {}", e))
                                }
                            }
                            Err(_) => Err(eyre!("Unable to parse response: {}", e)),
                        }
                    }
                }
            }
            Err(e) => Err(eyre!("Unable to connect to doublezero daemon: {}", e)),
        }
    }
}
