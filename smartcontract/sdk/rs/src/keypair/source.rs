use std::{fmt, path::PathBuf};

/// Represents the source from which a keypair was loaded.
/// Used for provenance tracking and debugging.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum KeypairSource {
    /// Keypair loaded from CLI argument (highest precedence)
    CliArgument(PathBuf),
    /// Keypair loaded from DOUBLEZERO_KEYPAIR environment variable
    EnvVar {
        /// Whether the env var contained raw JSON or a file path
        is_json: bool,
    },
    /// Keypair loaded from stdin (piped input)
    Stdin,
    /// Keypair loaded from config file keypair_path
    ConfigFile(PathBuf),
    /// Keypair loaded from default path
    DefaultPath(PathBuf),
}

impl fmt::Display for KeypairSource {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::CliArgument(path) => write!(f, "CLI argument ({})", path.display()),
            Self::EnvVar { is_json: true } => {
                write!(f, "DOUBLEZERO_KEYPAIR env var (JSON content)")
            }
            Self::EnvVar { is_json: false } => write!(f, "DOUBLEZERO_KEYPAIR env var (file path)"),
            Self::Stdin => write!(f, "stdin"),
            Self::ConfigFile(path) => write!(f, "config file ({})", path.display()),
            Self::DefaultPath(path) => write!(f, "default path ({})", path.display()),
        }
    }
}
