use thiserror::Error;

#[derive(Error, Debug)]
pub enum FetchError {
    #[error("RPC error: {0}")]
    Rpc(String),

    #[error("Deserialization error: {0}")]
    Deserialization(String),

    #[error("No accounts found for the specified criteria")]
    NoAccountsFound,

    #[error("Invalid epoch: {0}")]
    InvalidEpoch(String),

    #[error("Configuration error: {0}")]
    Configuration(String),
}
