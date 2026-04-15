use std::{
    error::Error as StdError,
    future::Future,
    sync::atomic::{AtomicUsize, Ordering},
    time::Duration,
};

use backon::{ExponentialBuilder, Retryable};
use solana_client::client_error::{
    reqwest::{Error as ReqwestError, StatusCode},
    ClientError, ClientErrorKind,
};
use thiserror::Error;
use tracing::warn;

pub type Result<T = ()> = std::result::Result<T, SentinelError>;

#[derive(Debug, Error, strum_macros::IntoStaticStr)]
pub enum SentinelError {
    #[error("deserialization error: {0}")]
    Deserialize(String),
    #[error("rpc client error: {0}")]
    RpcClient(Box<ClientError>),
}

impl From<ClientError> for SentinelError {
    fn from(err: ClientError) -> Self {
        SentinelError::RpcClient(Box::new(err))
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
        .with_max_times(10)
        .with_jitter();

    let result = (move || op())
        .retry(backoff)
        .when(|err: &SentinelError| should_retry(err))
        .notify(|err: &SentinelError, delay: Duration| {
            let attempt = attempts.fetch_add(1, Ordering::Relaxed) + 1;
            let error_type = classify_rpc_error(err);

            metrics::counter!(
                "doublezero_sentinel_rpc_retry_total",
                "operation" => label,
                "error_type" => error_type
            )
            .increment(1);

            warn!(attempt, retry_in = ?delay, error = ?err, operation = label, "transient RPC failure");
        })
        .await;

    if let Err(ref err) = result {
        let error_type = classify_rpc_error(err);
        metrics::counter!(
            "doublezero_sentinel_rpc_retry_exhausted_total",
            "operation" => label,
            "error_type" => error_type
        )
        .increment(1);
    }

    result
}

fn should_retry(err: &SentinelError) -> bool {
    match err {
        SentinelError::RpcClient(client_err) => retryable_client_error(client_err.as_ref()),
        _ => false,
    }
}

fn retryable_client_error(err: &ClientError) -> bool {
    match err.kind() {
        ClientErrorKind::Reqwest(reqwest_err) => {
            if reqwest_err.is_timeout()
                || reqwest_err.is_connect()
                || reqwest_err.is_request()
                || is_connection_reset(reqwest_err)
            {
                return true;
            }
            retryable_status(reqwest_err.status())
        }
        _ => false,
    }
}

fn is_connection_reset(reqwest_err: &ReqwestError) -> bool {
    let mut source = reqwest_err.source();

    while let Some(err) = source {
        if let Some(io_err) = err.downcast_ref::<std::io::Error>() {
            if io_err.kind() == std::io::ErrorKind::ConnectionReset
                || io_err.kind() == std::io::ErrorKind::BrokenPipe
            {
                return true;
            }
        }
        source = err.source();
    }

    false
}

fn classify_rpc_error(err: &SentinelError) -> &'static str {
    match err {
        SentinelError::RpcClient(client_err) => classify_client_error(client_err.as_ref()),
        _ => err.into(),
    }
}

fn classify_client_error(err: &ClientError) -> &'static str {
    match err.kind() {
        ClientErrorKind::Reqwest(reqwest_err) => {
            if reqwest_err.is_timeout() {
                return "timeout";
            }
            if reqwest_err.is_connect() {
                return "connect";
            }
            if is_connection_reset(reqwest_err) {
                return "connection_reset";
            }
            if reqwest_err.is_request() {
                return "request";
            }
            if reqwest_err.status().is_some() {
                return "http_status";
            }
            "reqwest"
        }
        ClientErrorKind::Io(_) => "io",
        ClientErrorKind::TransactionError(_) => "transaction",
        ClientErrorKind::RpcError(_) => "rpc",
        ClientErrorKind::SigningError(_) => "signing",
        ClientErrorKind::SerdeJson(_) => "serde_json",
        ClientErrorKind::Custom(_) => "custom",
        ClientErrorKind::Middleware(_) => "middleware",
    }
}

fn retryable_status(status: Option<StatusCode>) -> bool {
    match status {
        Some(code) => {
            code.is_server_error()
                || code == StatusCode::TOO_MANY_REQUESTS
                || code == StatusCode::FORBIDDEN
        }
        None => false,
    }
}

#[cfg(test)]
mod tests {
    use solana_sdk::transaction::TransactionError;

    use super::*;

    #[test]
    fn retryable_status_codes() {
        assert!(retryable_status(Some(StatusCode::INTERNAL_SERVER_ERROR)));
        assert!(retryable_status(Some(StatusCode::TOO_MANY_REQUESTS)));
        assert!(retryable_status(Some(StatusCode::SERVICE_UNAVAILABLE)));
        assert!(retryable_status(Some(StatusCode::FORBIDDEN)));
        assert!(!retryable_status(Some(StatusCode::BAD_REQUEST)));
        assert!(!retryable_status(None));
    }

    #[test]
    fn does_not_retry_transaction_errors() {
        let err = SentinelError::from(ClientError::from(TransactionError::AccountNotFound));
        assert!(!should_retry(&err));
    }

    #[test]
    fn classify_non_rpc_errors_by_variant() {
        let err = SentinelError::Deserialize("test".into());
        assert_eq!(classify_rpc_error(&err), "Deserialize");
    }

    #[test]
    fn classify_transaction_errors() {
        let err = SentinelError::from(ClientError::from(TransactionError::AccountNotFound));
        assert_eq!(classify_rpc_error(&err), "transaction");
    }
}
