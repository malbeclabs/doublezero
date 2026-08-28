use std::{fmt, path::PathBuf};

/// Represents the source from which a keypair was loaded.
/// Used for provenance tracking and debugging.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum KeypairSource {
    /// Keypair loaded from CLI argument (highest precedence)
    CliArgument(PathBuf),
    /// Keypair loaded from stdin (piped input)
    Stdin,
    /// Keypair loaded from default path
    DefaultPath(PathBuf),
}

impl fmt::Display for KeypairSource {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::CliArgument(path) => write!(f, "CLI argument ({})", path.display()),
            Self::Stdin => write!(f, "stdin"),
            Self::DefaultPath(path) => write!(f, "default path ({})", path.display()),
        }
    }
}
