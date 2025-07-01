use crate::{
    metrics_service::{Metric, MetricsService},
    utils::{get_utc_nanoseconds_since_epoch, kvpair_string},
};
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
) -> eyre::Result<(
    Box<dyn MetricsService + Send + Sync>,
    InfluxDBMetricsSubmitter,
)> {
    let (tx, rx) = tokio::sync::mpsc::channel(16);
    Ok((
        Box::new(InfluxDBMetricsService { sender: tx }),
        InfluxDBMetricsSubmitter::new(host, org, token, bucket, rx)?,
    ))
}

impl InfluxDBMetricsService {
    pub fn metric_to_line_proto(metric: &Metric) -> eyre::Result<String> {
        let ts = get_utc_nanoseconds_since_epoch()?;
        if metric.tags.is_empty() {
            let msg = format!(
                "{} {} {}\n",
                metric.measurement,
                kvpair_string(&metric.fields),
                ts
            );
            return Ok(msg);
        }

        let msg = format!(
            "{},{} {} {}\n",
            metric.measurement,
            kvpair_string(&metric.tags),
            kvpair_string(&metric.fields),
            ts
        );
        Ok(msg)
    }

    fn send(&self, lines: String) {
        _ = self.sender.blocking_send(lines).inspect_err(|err| eprintln!("Error sending metrics: {err}"));
    }
}

impl InfluxDBMetricsSubmitter {
    pub fn new(
        host: Option<&str>,
        org: Option<&str>,
        token: Option<&str>,
        bucket: Option<&str>,
        receiver: Receiver<String>,
    ) -> eyre::Result<Self> {
        match host {
            Some(host) => Ok(InfluxDBMetricsSubmitter {
                client: Some(Client::new(
                    host,
                    org.ok_or_else(|| eyre::eyre!("Influx org required"))?,
                    token.ok_or_else(|| eyre::eyre!("Influx token required"))?,
                )),
                bucket: bucket
                    .ok_or_else(|| eyre::eyre!("Influx bucket required"))?
                    .to_string(),
                receiver,
            }),
            None => Ok(InfluxDBMetricsSubmitter {
                client: None,
                bucket: "".to_owned(),
                receiver,
            }),
        }
    }

    pub async fn run(&mut self) {
        while let Some(msg) = self.receiver.recv().await {
            if let Some(client) = &self.client {
                if let Err(e) = client
                    .write_line_protocol(&client.org, self.bucket.as_str(), msg)
                    .await
                {
                    eprintln!("Error writing metric to InfluxDB: {e}");
                }
            }
        }
    }
}

impl MetricsService for InfluxDBMetricsService {
    fn write_metrics(&self, metrics: &[Metric]) -> eyre::Result<()> {
        let lines = metrics
            .iter()
            .map(Self::metric_to_line_proto)
            .collect::<eyre::Result<Vec<_>>>()?
            .join("");
        self.send(lines);
        Ok(())
    }
}
