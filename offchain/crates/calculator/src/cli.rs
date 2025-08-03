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

    #[command(subcommand)]
    pub command: Commands,
}

#[derive(Subcommand, Debug)]
pub enum Commands {
    /// Calculate epoch rewards
    CalculateRewards {
        /// If specified, rewards are calculated for `epoch-1`, otherwise `current_epoch - 1`
        #[arg(short, long)]
        epoch: Option<u64>,
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
