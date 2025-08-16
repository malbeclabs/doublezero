use crate::{client::solana::SolPubsubClient, Result};

use futures::StreamExt;
use solana_sdk::signature::Signature;
use tokio::sync::mpsc::{unbounded_channel, UnboundedReceiver, UnboundedSender};
use tokio_util::sync::CancellationToken;
use tracing::info;
use url::Url;

pub struct ReqListener {
    pubsub_client: SolPubsubClient,
    tx: UnboundedSender<Signature>,
}

impl ReqListener {
    pub async fn new(ws_url: Url) -> Result<(Self, UnboundedReceiver<Signature>)> {
        let (tx, rx) = unbounded_channel();
        Ok((
            Self {
                pubsub_client: SolPubsubClient::new(ws_url).await?,
                tx,
            },
            rx,
        ))
    }

    pub async fn run(&self, shutdown_listener: CancellationToken) -> Result<()> {
        info!("AccessRequest listener subscribing to logs");

        let (mut request_stream, subscription) =
            self.pubsub_client.subscribe_to_access_requests().await?;

        loop {
            tokio::select! {
                biased;
                _ = shutdown_listener.cancelled() => {
                    subscription().await;
                    break
                }
                req = request_stream.next() => {
                    if let Some(log_event) = req {
                        if log_event.value.logs.iter().any(|log| log.contains("Initialized user AccessRequest")) {
                            let signature: Signature = log_event.value.signature.parse()?;
                            self.tx.send(signature)?;
                            metrics::counter!("doublezero_sentinel_access_request_received").increment(1);
                        }
                    }
                }
            }
        }

        Ok(())
    }
}
