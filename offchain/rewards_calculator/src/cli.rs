use clap::Parser;

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
