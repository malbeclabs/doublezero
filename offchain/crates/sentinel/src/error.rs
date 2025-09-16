use backon::{ExponentialBuilder, Retryable};
use solana_client::{
    client_error::{ClientError, ClientErrorKind, reqwest::StatusCode},
    nonblocking::pubsub_client::PubsubClientError,
};
use solana_sdk::signature::{ParseSignatureError, Signature};
use std::{
    future::Future,
    sync::atomic::{AtomicUsize, Ordering},
    time::Duration,
};
use thiserror::Error;
use tracing::warn;

pub type Result<T = ()> = std::result::Result<T, Error>;

#[derive(Debug, Error)]
pub enum Error {
    #[error("base64 decode error: {0}")]
    Base64Decode(#[from] base64::DecodeError),
    #[error("bincode deserialization error: {0}")]
    BincodeDeser(#[from] bincode::Error),
    #[error("borsh deserialization error: {0}")]
    BorshIo(#[from] borsh::io::Error),
    #[error("instruction not found in transaction: {0}")]
    InstructionNotFound(Signature),
    #[error("invalid instruction data: {0}")]
    InstructionInvalid(Signature),
    #[error("no account keys for transaction ix: {0}")]
    MissingAccountKeys(Signature),
    #[error("no program id at expected instruction index: {0}")]
    MissingProgramId(Signature),
    #[error("no transaction id signature")]
    MissingTxnSignature,
    #[error("pubsub client error: {0}")]
    PubsubClient(Box<PubsubClientError>),
    #[error("request channel error: {0}")]
    ReqChannel(#[from] tokio::sync::mpsc::error::SendError<Signature>),
    #[error("rpc client error: {0}")]
    RpcClient(Box<ClientError>),
    #[error("invalid transaction signature: {0}")]
    SignatureInvalid(#[from] ParseSignatureError),
    #[error("access request signature did not verify")]
    SignatureVerify,
    #[error("invalid transaction encoding: {0}")]
    TransactionEncoding(Signature),
    #[error("solana offchain message error: {0}")]
    OffchainSanitize(#[from] solana_sanitize::SanitizeError),
}

impl From<ClientError> for Error {
    fn from(err: ClientError) -> Self {
        Error::RpcClient(Box::new(err))
    }
}

impl From<PubsubClientError> for Error {
    fn from(err: PubsubClientError) -> Self {
        Error::PubsubClient(Box::new(err))
    }
}

pub async fn rpc_with_retry<F, Fut, T>(operation: F, label: &'static str) -> Result<T>
where
    F: FnMut() -> Fut,
    Fut: Future<Output = Result<T>>,
{
    let mut op = operation;
    let attempts = AtomicUsize::new(0);
    let backoff = ExponentialBuilder::default()
        .with_min_delay(Duration::from_secs(1))
        .with_max_delay(Duration::from_secs(30))
        .with_max_times(8)
        .with_jitter();

    (move || op())
        .retry(backoff)
        .when(|err: &Error| should_retry(err))
        .notify(|err: &Error, delay: Duration| {
            let attempt = attempts.fetch_add(1, Ordering::Relaxed) + 1;
            warn!(attempt, retry_in = ?delay, error = ?err, operation = label, "transient RPC failure");
        })
        .await
}

fn should_retry(err: &Error) -> bool {
    match err {
        Error::RpcClient(client_err) => retryable_client_error(client_err.as_ref()),
        _ => false,
    }
}

fn retryable_client_error(err: &ClientError) -> bool {
    match err.kind() {
        ClientErrorKind::Reqwest(reqwest_err) => {
            if reqwest_err.is_timeout() || reqwest_err.is_connect() {
                return true;
            }
            retryable_status(reqwest_err.status())
        }
        _ => false,
    }
}

fn retryable_status(status: Option<StatusCode>) -> bool {
    match status {
        Some(code) => code.is_server_error() || code == StatusCode::TOO_MANY_REQUESTS,
        None => false,
    }
}

#[cfg(test)]
mod tests {
    use super::StatusCode;
    use super::*;
    use solana_sdk::transaction::TransactionError;

    #[test]
    fn retryable_status_codes() {
        // Minimally, 429, 500 and 503 should be retryable
        assert!(retryable_status(Some(StatusCode::INTERNAL_SERVER_ERROR)));
        assert!(retryable_status(Some(StatusCode::TOO_MANY_REQUESTS)));
        assert!(retryable_status(Some(StatusCode::SERVICE_UNAVAILABLE)));
        assert!(!retryable_status(Some(StatusCode::BAD_REQUEST)));
        assert!(!retryable_status(None));
    }

    #[test]
    fn does_not_retry_transaction_errors() {
        let err = Error::from(ClientError::from(TransactionError::AccountNotFound));
        assert!(!should_retry(&err));
    }
}
