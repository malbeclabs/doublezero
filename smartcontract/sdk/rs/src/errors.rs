use solana_rpc_client_api::{request, response};
use solana_sdk::{
    signature::SignerError, transaction::TransactionError, transport::TransportError,
};
use std::io;
use thiserror::Error as ThisError;

#[allow(clippy::large_enum_variant)]
#[derive(ThisError, Debug)]
pub enum ErrorKind {
    #[error(transparent)]
    Io(#[from] io::Error),
    #[error(transparent)]
    RpcError(#[from] solana_rpc_client_api::request::RpcError),
    #[error(transparent)]
    ClientError(#[from] solana_rpc_client_api::client_error::Error),
    #[error(transparent)]
    SerdeJson(#[from] serde_json::error::Error),
    #[error(transparent)]
    SigningError(#[from] SignerError),
    #[error(transparent)]
    TransactionError(#[from] TransactionError),
    #[error("Custom: {0}")]
    Custom(String),
}

impl ErrorKind {
    pub fn get_transaction_error(&self) -> Option<TransactionError> {
        match self {
            Self::RpcError(request::RpcError::RpcResponseError {
                data:
                    request::RpcResponseErrorData::SendTransactionPreflightFailure(
                        response::RpcSimulateTransactionResult {
                            err: Some(tx_err), ..
                        },
                    ),
                ..
            }) => Some(tx_err.clone().into()),
            Self::TransactionError(tx_err) => Some(tx_err.clone()),
            _ => None,
        }
    }
}

impl From<TransportError> for ErrorKind {
    fn from(err: TransportError) -> Self {
        match err {
            TransportError::IoError(err) => Self::Io(err),
            TransportError::TransactionError(err) => Self::TransactionError(err),
            TransportError::Custom(err) => Self::Custom(err),
        }
    }
}

impl From<ErrorKind> for TransportError {
    fn from(client_error_kind: ErrorKind) -> Self {
        match client_error_kind {
            ErrorKind::Io(err) => Self::IoError(err),
            ErrorKind::TransactionError(err) => Self::TransactionError(err),
            ErrorKind::RpcError(err) => Self::Custom(format!("{err:?}")),
            ErrorKind::ClientError(err) => Self::Custom(format!("{err:?}")),
            ErrorKind::SerdeJson(err) => Self::Custom(format!("{err:?}")),
            ErrorKind::SigningError(err) => Self::Custom(format!("{err:?}")),
            ErrorKind::Custom(err) => Self::Custom(format!("{err:?}")),
        }
    }
}

#[derive(ThisError, Debug)]
#[error("{kind}")]
pub struct Error {
    #[source]
    pub kind: ErrorKind,
}

impl Error {
    pub fn new(kind: ErrorKind) -> Self {
        Self { kind }
    }
    pub fn kind(&self) -> &ErrorKind {
        &self.kind
    }

    pub fn get_transaction_error(&self) -> Option<TransactionError> {
        self.kind.get_transaction_error()
    }
}

impl From<Error> for TransportError {
    fn from(client_error: Error) -> Self {
        client_error.kind.into()
    }
}

pub type Result<T> = std::result::Result<T, Error>;
