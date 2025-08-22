use crate::{Result, client::solana::SolPubsubClient};

use futures::StreamExt;
use solana_sdk::signature::Signature;
use tokio::sync::mpsc::{UnboundedReceiver, UnboundedSender, unbounded_channel};
use tokio_util::sync::CancellationToken;
use tracing::{debug, info};
use url::Url;

const ACCESS_REQ_INIT_LOG: &str = "Initialized user access request";

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

        loop {
            let (mut request_stream, subscription) =
                self.pubsub_client.subscribe_to_access_requests().await?;

            // Check the stream for new access requests and break on shutdown signals
            // If the stream returns a `None` then the server has disconnected and we resubscribe
            while let Some(log_event) = request_stream.next().await
                && !shutdown_listener.is_cancelled()
            {
                if log_event
                    .value
                    .logs
                    .iter()
                    .any(|log| log.contains(ACCESS_REQ_INIT_LOG))
                {
                    let signature: Signature = log_event.value.signature.parse()?;
                    self.tx.send(signature)?;
                    metrics::counter!("doublezero_sentinel_access_request_received").increment(1);
                }
            }

            if shutdown_listener.is_cancelled() {
                info!("shutdown signal detected; exiting access request listener");
                subscription().await;
                break;
            } else {
                debug!("pubsub server disconnected access request listener; reconnecting...");
            }
        }

        Ok(())
    }
}
