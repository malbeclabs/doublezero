use clap::{Parser, ValueEnum};

#[derive(Debug, Clone, Copy, ValueEnum)]
pub enum CacheFormat {
    /// Human-readable JSON format
    Json,
    /// Structured directory with separate files
    Structured,
}

#[derive(Parser, Debug)]
#[command(
    name = "doublezero-rewards-calculator",
    about = "Off-chain rewards calculation for DoubleZero network",
    version,
    author
)]
pub struct Cli {
    /// End timestamp for the rewards period (required)
    /// Accepts: ISO 8601 (2024-01-15T10:00:00Z), Unix timestamp (1705315200), or relative time (2 hours ago)
    #[arg(short, long, help_heading = "Time Range")]
    pub before: String,

    /// Start timestamp for the rewards period (required)
    /// Accepts: ISO 8601 (2024-01-15T08:00:00Z), Unix timestamp (1705308000), or relative time (4 hours ago)
    #[arg(short, long, help_heading = "Time Range")]
    pub after: String,

    /// Override log level (trace, debug, info, warn, error)
    #[arg(short, long)]
    pub log_level: Option<String>,

    /// Override RPC URL
    #[arg(short, long)]
    pub rpc_url: Option<String>,

    /// Skip third-party data fetching
    #[arg(short, long)]
    pub skip_third_party: bool,

    /// Directory to save cache files for inspection
    #[arg(long, value_name = "DIR", conflicts_with = "load_cache")]
    pub cache_dir: Option<String>,

    /// Load data from cache directory instead of fetching
    #[arg(long, value_name = "DIR", conflicts_with = "cache_dir")]
    pub load_cache: Option<String>,

    /// Include processed metrics and Shapley inputs in cache
    #[arg(long, requires = "cache_dir")]
    pub cache_processed: bool,

    /// Cache format: json or structured (default: json)
    #[arg(long, value_enum, default_value = "json")]
    pub cache_format: CacheFormat,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_cli_parsing() {
        let args = vec![
            "doublezero-rewards-calculator",
            "--before",
            "2024-01-15T10:00:00Z",
            "--after",
            "2024-01-15T08:00:00Z",
            "--log-level",
            "debug",
        ];

        let cli = Cli::try_parse_from(args).unwrap();
        assert_eq!(cli.before, "2024-01-15T10:00:00Z");
        assert_eq!(cli.after, "2024-01-15T08:00:00Z");
        assert_eq!(cli.log_level, Some("debug".to_string()));
    }

    #[test]
    fn test_unix_timestamp() {
        let args = vec![
            "doublezero-rewards-calculator",
            "--before",
            "1705315200",
            "--after",
            "1705308000",
        ];

        let cli = Cli::try_parse_from(args).unwrap();
        assert_eq!(cli.before, "1705315200");
        assert_eq!(cli.after, "1705308000");
    }

    #[test]
    fn test_relative_time() {
        let args = vec![
            "doublezero-rewards-calculator",
            "--before",
            "2 hours ago",
            "--after",
            "4 hours ago",
        ];

        let cli = Cli::try_parse_from(args).unwrap();
        assert_eq!(cli.before, "2 hours ago");
        assert_eq!(cli.after, "4 hours ago");
    }

    #[test]
    fn test_required_timestamps() {
        let args = vec!["doublezero-rewards-calculator"];
        let result = Cli::try_parse_from(args);
        assert!(result.is_err());
    }
}
