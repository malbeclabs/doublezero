//! Epoch monitoring with WebSocket primary and polling fallback

use crate::Result;
use futures::StreamExt;
use solana_client::nonblocking::{pubsub_client::PubsubClient, rpc_client::RpcClient};
use std::{
    sync::{
        atomic::{AtomicU64, Ordering},
        Arc,
    },
    time::Duration,
};
use tokio::sync::mpsc;
use tokio_util::sync::CancellationToken;
use tracing::{error, info, warn};

/// Solana mainnet constant
const SLOTS_PER_EPOCH: u64 = 432_000;

/// Epoch change event
#[derive(Debug, Clone)]
pub struct EpochChange {
    pub new_epoch: u64,
    pub slot: u64,
    pub timestamp: i64,
}

/// Combined epoch monitor that tries WebSocket first, falls back to polling
pub struct EpochMonitor {
    rpc_url: String,
    ws_url: Option<String>,
    current_epoch: Arc<AtomicU64>,
    epoch_tx: mpsc::UnboundedSender<EpochChange>,
}

impl EpochMonitor {
    pub fn new(
        rpc_url: String,
        ws_url: Option<String>,
    ) -> (Self, mpsc::UnboundedReceiver<EpochChange>) {
        let (epoch_tx, epoch_rx) = mpsc::unbounded_channel();

        let monitor = Self {
            rpc_url,
            ws_url,
            current_epoch: Arc::new(AtomicU64::new(0)),
            epoch_tx,
        };

        (monitor, epoch_rx)
    }

    pub async fn run(&self, shutdown: CancellationToken) -> Result<()> {
        // Get initial epoch
        let initial = self.fetch_current_epoch().await?;
        self.current_epoch.store(initial, Ordering::Relaxed);
        info!("Starting epoch monitor at epoch {}", initial);

        // Try WebSocket if URL provided, otherwise use polling
        if let Some(ws_url) = &self.ws_url {
            info!("Using WebSocket monitoring at {}", ws_url);
            self.run_websocket(ws_url.clone(), shutdown).await
        } else {
            info!("Using polling for epoch monitoring");
            self.run_polling(shutdown).await
        }
    }

    async fn run_websocket(&self, ws_url: String, shutdown: CancellationToken) -> Result<()> {
        let mut reconnect_delay = Duration::from_secs(1);

        loop {
            match self.websocket_subscribe(&ws_url, shutdown.clone()).await {
                Ok(_) => {
                    if shutdown.is_cancelled() {
                        info!("Epoch monitor shutting down");
                        break;
                    }
                    // Reset delay on successful run
                    reconnect_delay = Duration::from_secs(1);
                }
                Err(e) => {
                    if shutdown.is_cancelled() {
                        break;
                    }

                    error!(
                        "WebSocket error: {}, reconnecting in {:?}",
                        e, reconnect_delay
                    );
                    tokio::time::sleep(reconnect_delay).await;

                    // Exponential backoff up to 30 seconds
                    reconnect_delay = (reconnect_delay * 2).min(Duration::from_secs(30));

                    // Resync state after disconnect
                    if let Err(e) = self.resync_after_disconnect().await {
                        error!("Failed to resync after disconnect: {}", e);
                    }
                }
            }
        }

        Ok(())
    }

    async fn websocket_subscribe(&self, ws_url: &str, shutdown: CancellationToken) -> Result<()> {
        let pubsub = PubsubClient::new(ws_url)
            .await
            .map_err(|e| crate::Error::PubSubClient(e.to_string()))?;
        let (mut stream, unsubscribe) = pubsub
            .slot_subscribe()
            .await
            .map_err(|e| crate::Error::PubSubClient(e.to_string()))?;

        while let Some(slot_info) = stream.next().await {
            if shutdown.is_cancelled() {
                unsubscribe().await;
                return Ok(());
            }

            let slot_epoch = slot_info.slot / SLOTS_PER_EPOCH;
            let current = self.current_epoch.load(Ordering::Relaxed);

            if slot_epoch > current {
                info!("Epoch changed: {} -> {}", current, slot_epoch);
                self.handle_epoch_change(slot_epoch, slot_info.slot).await;
            }
        }

        Err(crate::Error::WebSocket("Stream ended".into()))
    }

    async fn run_polling(&self, shutdown: CancellationToken) -> Result<()> {
        let client = RpcClient::new(self.rpc_url.clone());

        loop {
            if shutdown.is_cancelled() {
                info!("Epoch monitor shutting down");
                break;
            }

            // Check for epoch change
            match client.get_epoch_info().await {
                Ok(info) => {
                    let current = self.current_epoch.load(Ordering::Relaxed);
                    if info.epoch > current {
                        info!("Epoch changed (polling): {} -> {}", current, info.epoch);
                        self.handle_epoch_change(info.epoch, info.absolute_slot)
                            .await;
                    }

                    // Adaptive delay based on position in epoch
                    let delay = calculate_poll_delay(&info);
                    tokio::time::sleep(delay).await;
                }
                Err(e) => {
                    error!("Failed to poll epoch: {}", e);
                    tokio::time::sleep(Duration::from_secs(30)).await;
                }
            }
        }

        Ok(())
    }

    async fn handle_epoch_change(&self, new_epoch: u64, slot: u64) {
        self.current_epoch.store(new_epoch, Ordering::Relaxed);

        let change = EpochChange {
            new_epoch,
            slot,
            timestamp: chrono::Utc::now().timestamp(),
        };

        if self.epoch_tx.send(change).is_err() {
            warn!("Failed to send epoch change notification");
        }

        metrics::counter!("scheduler_epoch_changes").increment(1);
    }

    async fn fetch_current_epoch(&self) -> Result<u64> {
        let client = RpcClient::new(self.rpc_url.clone());
        let info = client.get_epoch_info().await.map_err(Box::new)?;
        Ok(info.epoch)
    }

    async fn resync_after_disconnect(&self) -> Result<()> {
        let current_epoch = self.fetch_current_epoch().await?;
        let last_known = self.current_epoch.load(Ordering::Relaxed);

        if current_epoch > last_known {
            warn!(
                "Missed {} epochs during disconnect",
                current_epoch - last_known
            );

            // Notify about all missed epochs
            for epoch in (last_known + 1)..=current_epoch {
                let change = EpochChange {
                    new_epoch: epoch,
                    slot: epoch * SLOTS_PER_EPOCH,
                    timestamp: chrono::Utc::now().timestamp(),
                };
                let _ = self.epoch_tx.send(change);
            }

            self.current_epoch.store(current_epoch, Ordering::Relaxed);
        }

        Ok(())
    }
}

/// Calculate adaptive polling delay based on position in epoch
fn calculate_poll_delay(info: &solana_sdk::epoch_info::EpochInfo) -> Duration {
    let slots_remaining = info.slots_in_epoch.saturating_sub(info.slot_index);
    let est_seconds = (slots_remaining * 400) / 1000; // ~400ms per slot

    match est_seconds {
        s if s > 3600 => Duration::from_secs(60), // Far: poll every minute
        s if s > 300 => Duration::from_secs(30),  // Near: poll every 30s
        _ => Duration::from_secs(10),             // Very near: poll every 10s
    }
}
