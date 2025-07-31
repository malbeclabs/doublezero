use crate::{error::FetchError, rpc::RpcClientWithRetry};
use solana_epoch_info::EpochInfo;
use std::sync::Arc;

/// Get the current epoch information from the RPC
pub async fn get_current_epoch(rpc_client: &Arc<RpcClientWithRetry>) -> Result<u64, FetchError> {
    let epoch_info = rpc_client
        .client
        .get_epoch_info()
        .await
        .map_err(|e| FetchError::Rpc(format!("Failed to get epoch info: {e}")))?;

    Ok(epoch_info.epoch)
}

/// Get the previous epoch number
pub async fn get_previous_epoch(rpc_client: &Arc<RpcClientWithRetry>) -> Result<u64, FetchError> {
    let current_epoch = get_current_epoch(rpc_client).await?;

    if current_epoch == 0 {
        return Err(FetchError::InvalidEpoch(
            "Cannot get previous epoch for epoch 0".to_string(),
        ));
    }

    Ok(current_epoch - 1)
}

/// Get full epoch information from the RPC
pub async fn get_epoch_info(rpc_client: &Arc<RpcClientWithRetry>) -> Result<EpochInfo, FetchError> {
    rpc_client
        .client
        .get_epoch_info()
        .await
        .map_err(|e| FetchError::Rpc(format!("Failed to get epoch info: {e}")))
}
