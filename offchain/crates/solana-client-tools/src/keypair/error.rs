use thiserror::Error;

/// Error type for keypair loading operations.
#[derive(Debug, Error)]
pub enum KeypairLoadError {
    /// No keypair source was available
    #[error("No keypair source available. Tried:\n{}\n\nHint: Provide keypair via:\n  - doublezero-solana --keypair /path/to/key.json\n  - cat key.json | doublezero-solana ...", format_attempted(.attempted))]
    NoSourceAvailable {
        /// List of sources that were attempted
        attempted: Vec<String>,
    },

    /// Failed to read keypair from stdin
    #[error("Failed to read keypair from stdin: {message}")]
    StdinReadError {
        /// Error message
        message: String,
    },

    /// Failed to read keypair file
    #[error("Failed to read keypair file '{path}': {message}")]
    FileReadError {
        /// Path that was attempted
        path: String,
        /// Error message
        message: String,
    },

    /// Invalid JSON format in keypair data
    #[error("Invalid keypair JSON format from {origin}: {message}")]
    InvalidJsonFormat {
        /// Source description
        origin: String,
        /// Error message
        message: String,
    },

    /// Invalid keypair bytes (not 64 bytes)
    #[error("Invalid keypair bytes from {origin}: expected 64 bytes")]
    InvalidKeypairBytes {
        /// Source description
        origin: String,
    },

    /// Stdin is a TTY, cannot read interactively
    #[error(
        "Stdin is a TTY - cannot read keypair interactively. Pipe keypair JSON via stdin or use --keypair"
    )]
    StdinIsTty,

    /// Could not determine home directory
    #[error("Could not determine home directory for default keypair path")]
    HomeDirNotFound,
}

fn format_attempted(attempted: &[String]) -> String {
    attempted
        .iter()
        .enumerate()
        .map(|(i, s)| format!("  {}. {}", i + 1, s))
        .collect::<Vec<_>>()
        .join("\n")
}
