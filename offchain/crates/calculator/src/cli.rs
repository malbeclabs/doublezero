use clap::{Parser, Subcommand};
use std::path::PathBuf;

#[derive(Parser, Debug)]
#[command(
    name = "rewards-calculator",
    about = "Off-chain rewards calculation for DoubleZero network",
    version,
    author
)]
pub struct Cli {
    /// Override log level (trace, debug, info, warn, error)
    #[arg(short, long, global = true)]
    pub log_level: Option<String>,

    /// Override RPC URL
    #[arg(short, long, global = true)]
    pub rpc_url: Option<String>,

    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Subcommand, Debug)]
pub enum Commands {
    /// Calculate rewards for the given time period
    CalculateRewards {
        /// End timestamp for the rewards period (required)
        /// Accepts: ISO 8601 (2024-01-15T10:00:00Z), Unix timestamp (1705315200), or relative time (2 hours ago)
        #[arg(short, long, help_heading = "Time Range")]
        before: String,

        /// Start timestamp for the rewards period (required)
        /// Accepts: ISO 8601 (2024-01-15T08:00:00Z), Unix timestamp (1705308000), or relative time (4 hours ago)
        #[arg(short, long, help_heading = "Time Range")]
        after: String,
    },
    /// Export demand matrix and enriched validators to CSV files
    ExportDemand {
        /// Output path for demand CSV file
        #[arg(long)]
        demand: PathBuf,

        /// Output path for enriched validators CSV file (optional)
        #[arg(long)]
        enriched_validators: Option<PathBuf>,
    },
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_calculate_rewards_parsing() {
        let args = vec![
            "doublezero-rewards-calculator",
            "--log-level",
            "debug",
            "calculate-rewards",
            "--before",
            "2024-01-15T10:00:00Z",
            "--after",
            "2024-01-15T08:00:00Z",
        ];

        let cli = Cli::try_parse_from(args).unwrap();
        assert_eq!(cli.log_level, Some("debug".to_string()));
        match cli.command {
            Commands::CalculateRewards { before, after } => {
                assert_eq!(before, "2024-01-15T10:00:00Z");
                assert_eq!(after, "2024-01-15T08:00:00Z");
            }
            _ => panic!("Expected CalculateRewards command"),
        }
    }

    #[test]
    fn test_unix_timestamp() {
        let args = vec![
            "doublezero-rewards-calculator",
            "calculate-rewards",
            "--before",
            "1705315200",
            "--after",
            "1705308000",
        ];

        let cli = Cli::try_parse_from(args).unwrap();
        match cli.command {
            Commands::CalculateRewards { before, after } => {
                assert_eq!(before, "1705315200");
                assert_eq!(after, "1705308000");
            }
            _ => panic!("Expected CalculateRewards command"),
        }
    }

    #[test]
    fn test_relative_time() {
        let args = vec![
            "doublezero-rewards-calculator",
            "calculate-rewards",
            "--before",
            "2 hours ago",
            "--after",
            "4 hours ago",
        ];

        let cli = Cli::try_parse_from(args).unwrap();
        match cli.command {
            Commands::CalculateRewards { before, after } => {
                assert_eq!(before, "2 hours ago");
                assert_eq!(after, "4 hours ago");
            }
            _ => panic!("Expected CalculateRewards command"),
        }
    }

    #[test]
    fn test_export_demand_parsing() {
        let args = vec![
            "doublezero-rewards-calculator",
            "export-demand",
            "--demand",
            "demand.csv",
            "--enriched-validators",
            "validators.csv",
        ];

        let cli = Cli::try_parse_from(args).unwrap();
        match cli.command {
            Commands::ExportDemand {
                demand,
                enriched_validators,
            } => {
                assert_eq!(demand, PathBuf::from("demand.csv"));
                assert_eq!(enriched_validators, Some(PathBuf::from("validators.csv")));
            }
            _ => panic!("Expected ExportDemand command"),
        }
    }

    #[test]
    fn test_required_subcommand() {
        let args = vec!["doublezero-rewards-calculator"];
        let result = Cli::try_parse_from(args);
        assert!(result.is_err());
    }
}
