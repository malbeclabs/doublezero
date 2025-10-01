use crate::{Result, client::solana::SolPubsubClient};
use futures::StreamExt;
use solana_sdk::signature::Signature;
use std::time::Duration;
use tokio::{
    sync::mpsc::{UnboundedReceiver, UnboundedSender, unbounded_channel},
    time::sleep,
};
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};
use url::Url;

const ACCESS_REQ_INIT_LOG: &str = "Initialized user access request";
const MAX_RETRY_DELAY: Duration = Duration::from_secs(30);
const RETRY_BACKOFF_MULTIPLIER: u32 = 2;
const ERROR_AFTER_RETRIES: u32 = 3;

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

        let mut retry_delay = Duration::from_secs(1);
        let mut retry_count = 0;

        loop {
            // Check for shutdown before attempting subscription
            if shutdown_listener.is_cancelled() {
                info!(
                    "shutdown signal detected before subscription; exiting access request listener"
                );
                break;
            }

            // Attempt to subscribe with error handling
            let (mut request_stream, subscription) = match self
                .pubsub_client
                .subscribe_to_access_requests()
                .await
            {
                Ok(result) => {
                    // Reset retry state on successful connection
                    retry_delay = Duration::from_secs(1);
                    retry_count = 0;
                    metrics::counter!("doublezero_sentinel_pubsub_connected").increment(1);
                    result
                }
                Err(err) => {
                    retry_count += 1;
                    metrics::counter!("doublezero_sentinel_pubsub_connection_failed").increment(1);

                    // Only warn if we haven't exceeded ERROR_AFTER_RETRIES
                    if retry_count <= ERROR_AFTER_RETRIES {
                        warn!(
                            ?err,
                            ?retry_delay,
                            retry_count,
                            "failed to subscribe to access requests; retrying after delay (transient)"
                        );
                    } else {
                        error!(
                            ?err,
                            ?retry_delay,
                            retry_count,
                            "failed to subscribe to access requests after multiple retries (persistent issue)"
                        );
                    }

                    // Sleep with backoff before retrying
                    sleep(retry_delay).await;

                    // Exponential backoff with max cap
                    retry_delay =
                        std::cmp::min(retry_delay * RETRY_BACKOFF_MULTIPLIER, MAX_RETRY_DELAY);

                    continue;
                }
            };

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

                    // Handle channel send errors gracefully
                    if let Err(err) = self.tx.send(signature) {
                        error!(
                            ?err,
                            "failed to send signature to handler channel; handler stopped (channel receiver dropped)"
                        );
                        metrics::counter!("doublezero_sentinel_channel_send_failed").increment(1);
                        // Channel receiver dropped, maybe handler crashed, exit listener
                        subscription().await;
                        return Err(err.into());
                    }

                    metrics::counter!("doublezero_sentinel_access_request_received").increment(1);
                }
            }

            if shutdown_listener.is_cancelled() {
                info!("shutdown signal detected; exiting access request listener");
                subscription().await;
                break;
            } else {
                warn!("pubsub server disconnected access request listener; reconnecting...");
                metrics::counter!("doublezero_sentinel_pubsub_disconnected").increment(1);
                subscription().await;
            }
        }

        Ok(())
    }
}
