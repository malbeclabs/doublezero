use crate::metrics_service::{Metric, MetricsService};
use crate::utils::{get_utc_nanoseconds_since_epoch, kvpair_string};
use influxdb2::Client;
use tokio::sync::mpsc::{Receiver, Sender};

pub struct InfluxDBMetricsService {
    sender: Sender<String>,
}

pub struct InfluxDBMetricsSubmitter {
    client: Option<Client>,
    bucket: String,
    receiver: Receiver<String>,
}

pub fn create_influxdb_metrics_service(
    host: Option<&str>,
    org: Option<&str>,
    token: Option<&str>,
    bucket: Option<&str>,
) -> (
    Box<dyn MetricsService + Send + Sync>,
    InfluxDBMetricsSubmitter,
) {
    let (tx, rx) = tokio::sync::mpsc::channel(16);
    (
        Box::new(InfluxDBMetricsService { sender: tx }),
        InfluxDBMetricsSubmitter::new(host, org, token, bucket, rx),
    )
}

impl InfluxDBMetricsService {
    pub fn metric_to_line_proto(metric: &Metric) -> String {
        let ts = get_utc_nanoseconds_since_epoch();
        if metric.tags.is_empty() {
            return format!(
                "{} {} {}\n",
                metric.measurement,
                kvpair_string(&metric.fields),
                ts
            );
        }

        format!(
            "{},{} {} {}\n",
            metric.measurement,
            kvpair_string(&metric.tags),
            kvpair_string(&metric.fields),
            ts
        )
    }

    fn send(&self, lines: String) {
        match self.sender.blocking_send(lines) {
            Ok(_) => {}
            Err(e) => {
                eprintln!("Error sending metrics: {}", e);
            }
        }
    }
}

impl InfluxDBMetricsSubmitter {
    pub fn new(
        host: Option<&str>,
        org: Option<&str>,
        token: Option<&str>,
        bucket: Option<&str>,
        receiver: Receiver<String>,
    ) -> Self {
        match host {
            Some(host) => InfluxDBMetricsSubmitter {
                client: Some(Client::new(
                    host,
                    org.expect("Influx org required"),
                    token.expect("Influx token required"),
                )),
                bucket: bucket.expect("Influx bucket required").to_string(),
                receiver,
            },
            None => InfluxDBMetricsSubmitter {
                client: None,
                bucket: "".to_owned(),
                receiver,
            },
        }
    }

    pub async fn run(&mut self) {
        while let Some(msg) = self.receiver.recv().await {
            match &self.client {
                None => {}
                Some(client) => {
                    if let Err(e) = client
                                            .write_line_protocol(&client.org, self.bucket.as_str(), msg)
                                            .await {
                        eprintln!("Error writing metric to InfluxDB: {}", e);
                    }
                }
            }
        }
    }
}

impl MetricsService for InfluxDBMetricsService {
    fn write_metric(&self, metric: &Metric) {
        self.send(Self::metric_to_line_proto(metric));
    }

    fn write_metrics(&self, metrics: &[Metric]) {
        let lines = metrics
            .iter()
            .map(Self::metric_to_line_proto)
            .collect::<Vec<_>>()
            .join("");
        self.send(lines);
    }
}
