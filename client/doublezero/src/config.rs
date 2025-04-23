use clap::Args;
use clap::Subcommand;

use double_zero_cli::config::get::GetConfigArgs;
use double_zero_cli::config::set::SetConfigArgs;


#[derive(Args, Debug)]
pub struct ConfigArgs {
    #[command(subcommand)]
    pub command: ConfigCommands,
}

#[derive(Debug, Subcommand)]
pub enum ConfigCommands {
    #[command(about = "Get current config settings", hide = false)]
    Get(GetConfigArgs),
    #[command(about = "Set a config setting", hide = false)]
    Set(SetConfigArgs),
}