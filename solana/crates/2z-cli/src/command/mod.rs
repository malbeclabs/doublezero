mod admin;
mod ata;
mod contributor;
mod prepaid;
mod validator;

pub use admin::*;
pub use ata::*;
pub use contributor::*;
pub use prepaid::*;
pub use validator::*;

//

use anyhow::Result;
use clap::Subcommand;

#[derive(Debug, Subcommand)]
pub enum DoubleZero2zSolanaCommand {
    /// Admin commands.
    Admin(AdminCliCommand),

    /// Associated Token Account commands.
    Ata(AtaCliCommand),

    /// Network contributor reward commands.
    Contributor(ContributorCliCommand),

    /// Prepaid connection commands.
    Prepaid(PrepaidCliCommand),

    /// Solana validator commands.
    Validator(ValidatorCliCommand),
}

impl DoubleZero2zSolanaCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            DoubleZero2zSolanaCommand::Admin(admin) => admin.command.try_into_execute().await?,
            DoubleZero2zSolanaCommand::Ata(ata) => match ata.command {
                AtaSubCommand::Create {
                    recipient,
                    solana_payer_options,
                } => {
                    println!("Create");
                    println!("  recipient: {recipient}");
                    println!("  solana_payer_options: {solana_payer_options:?}");
                }
                AtaSubCommand::Fetch {
                    recipient,
                    solana_connection_options,
                } => {
                    println!("Fetch");
                    println!("  recipient: {recipient}");
                    println!("  solana_connection_options: {solana_connection_options:?}");
                }
            },
            DoubleZero2zSolanaCommand::Contributor(contributor) => match contributor.command {
                ContributorSubCommand::Claim {
                    service_key,
                    epoch_share,
                    rewards_from_file,
                    solana_payer_options,
                } => {
                    println!("Claim");
                    println!("  service_key: {service_key:?}");
                    println!("  epoch_share: {epoch_share:?}");
                    println!("  rewards_from_file: {rewards_from_file:?}");
                    println!("  solana_payer_options: {solana_payer_options:?}");
                }
                ContributorSubCommand::ComputeRewards {
                    epoch,
                    out_filename,
                    dz_ledger_rpc_options,
                } => {
                    println!("ComputeRewards");
                    println!("  epoch: {epoch}");
                    println!("  out_filename: {out_filename:?}");
                    println!("  dz_ledger_rpc_options: {dz_ledger_rpc_options:?}");
                }
                ContributorSubCommand::Configure {
                    service_key,
                    recipient_shares,
                    solana_payer_options,
                } => {
                    println!("Configure");
                    println!("  service_key: {service_key:?}");
                    println!("  recipient_shares: {recipient_shares:?}");
                    println!("  solana_payer_options: {solana_payer_options:?}");
                }
                ContributorSubCommand::Fetch {
                    service_key,
                    solana_connection_options,
                } => {
                    println!("Fetch");
                    println!("  service_key: {service_key:?}");
                    println!("  solana_connection_options: {solana_connection_options:?}");
                }
                ContributorSubCommand::FetchByManager {
                    rewards_manager_key,
                    solana_connection_options,
                } => {
                    println!("FetchByManager");
                    println!("  rewards_manager_key: {rewards_manager_key}");
                    println!("  solana_connection_options: {solana_connection_options:?}");
                }
                ContributorSubCommand::Initialize {
                    service_key,
                    solana_payer_options,
                } => {
                    println!("Initialize");
                    println!("  service_key: {service_key:?}");
                    println!("  solana_payer_options: {solana_payer_options:?}");
                }
            },
            DoubleZero2zSolanaCommand::Prepaid(prepaid) => match prepaid.command {
                PrepaidSubCommand::Initialize {
                    service_key,
                    solana_payer_options,
                } => {
                    println!("Initialize");
                    println!("  service_key: {service_key:?}");
                    println!("  solana_payer_options: {solana_payer_options:?}");
                }
                PrepaidSubCommand::Load {
                    service_key,
                    valid_through_epoch,
                    solana_payer_options,
                } => {
                    println!("Load");
                    println!("  service_key: {service_key:?}");
                    println!("  valid_through_epoch: {valid_through_epoch:?}");
                    println!("  solana_payer_options: {solana_payer_options:?}");
                }
            },
            DoubleZero2zSolanaCommand::Validator(validator) => match validator.command {
                ValidatorSubCommand::ComputeRevenue {
                    epoch,
                    out_filename,
                    solana_connection_options,
                } => {
                    println!("ComputeRevenue");
                    println!("  epoch: {epoch:?}");
                    println!("  out_filename: {out_filename:?}");
                    println!("  solana_connection_options: {solana_connection_options:?}");
                }
                ValidatorSubCommand::FetchComputedRevenue {
                    epoch,
                    out_filename,
                    dz_ledger_rpc_options,
                } => {
                    println!("FetchComputedRevenue");
                    println!("  epoch: {epoch:?}");
                    println!("  out_filename: {out_filename:?}");
                    println!("  dz_ledger_rpc_options: {dz_ledger_rpc_options:?}");
                }
                ValidatorSubCommand::PayFee {
                    validator_id,
                    epoch_revenue,
                    rewards_from_file,
                    solana_payer_options,
                } => {
                    println!("PayFee");
                    println!("  validator_id: {validator_id:?}");
                    println!("  epoch_revenue: {epoch_revenue:?}");
                    println!("  rewards_from_file: {rewards_from_file:?}");
                    println!("  solana_payer_options: {solana_payer_options:?}");
                }
                ValidatorSubCommand::RequestAccess {
                    validator_id,
                    service_key,
                    ed25519_signature,
                    solana_payer_options,
                } => {
                    println!("RequestAccess");
                    println!("  validator_id: {validator_id:?}");
                    println!("  service_key: {service_key:?}");
                    println!("  ed25519_signature: {ed25519_signature:?}");
                    println!("  solana_payer_options: {solana_payer_options:?}");
                }
            },
        }

        Ok(())
    }
}
