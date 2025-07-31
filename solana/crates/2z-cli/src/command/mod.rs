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

use crate::payer::SolanaPayerOptions;
use anyhow::Result;
use clap::Subcommand;
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Subcommand)]
pub enum DoubleZero2zSolanaCommand {
    /// Admin commands.
    Admin(AdminCliCommand),

    /// Associated Token Account commands.
    Ata(AtaCliCommand),

    /// Network contributor reward commands.
    Contributor(ContributorCliCommand),

    /// Initialize the journal account. This command can only be called once.
    InitializeJournal {
        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Initialize the program config account. This command can only be called once.
    InitializeProgram {
        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Prepaid connection commands.
    Prepaid(PrepaidCliCommand),

    /// Set the admin key. Only the upgrade authority can execute this command.
    SetAdmin {
        admin_key: Pubkey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Solana validator commands.
    Validator(ValidatorCliCommand),
}

impl DoubleZero2zSolanaCommand {
    pub fn into_execute(self) -> Result<()> {
        match self {
            DoubleZero2zSolanaCommand::Admin(admin) => match admin.command {
                AdminSubCommand::ConfigureJournal {
                    activation_cost,
                    cost_per_epoch,
                    payer_options,
                } => {
                    println!("ConfigureJournal");
                    println!("  activation_cost: {:?}", activation_cost);
                    println!("  cost_per_epoch: {:?}", cost_per_epoch);
                    println!("  payer_options: {:?}", payer_options);
                }
                AdminSubCommand::ConfigureProgram {
                    pause,
                    unpause,
                    accountant_key,
                    sol_2z_swap_program_id,
                    solana_validator_fee_percentage,
                    calculation_grace_period_seconds,
                    prepaid_connection_termination_relay_lamports,
                    payer_options,
                } => {
                    println!("ConfigureProgram");
                    println!(".. pause: {:?}", pause);
                    println!(".. unpause: {:?}", unpause);
                    println!(".. accountant_key: {:?}", accountant_key);
                    println!(".. sol_2z_swap_program_id: {:?}", sol_2z_swap_program_id);
                    println!(
                        ".. solana_validator_fee_percentage: {:?}",
                        solana_validator_fee_percentage
                    );
                    println!(
                        ".. calculation_grace_period_seconds: {:?}",
                        calculation_grace_period_seconds
                    );
                    println!(
                        ".. prepaid_connection_termination_relay_lamports: {:?}",
                        prepaid_connection_termination_relay_lamports
                    );
                    println!(".. payer_options: {:?}", payer_options);
                }
            },
            DoubleZero2zSolanaCommand::Ata(ata) => match ata.command {
                AtaSubCommand::Create {
                    recipient,
                    solana_payer_options,
                } => {
                    println!("Create");
                    println!("  recipient: {}", recipient);
                    println!("  solana_payer_options: {:?}", solana_payer_options);
                }
                AtaSubCommand::Fetch {
                    recipient,
                    solana_rpc_options,
                } => {
                    println!("Fetch");
                    println!("  recipient: {}", recipient);
                    println!("  solana_rpc_options: {:?}", solana_rpc_options);
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
                    println!("  service_key: {:?}", service_key);
                    println!("  epoch_share: {:?}", epoch_share);
                    println!("  rewards_from_file: {:?}", rewards_from_file);
                    println!("  solana_payer_options: {:?}", solana_payer_options);
                }
                ContributorSubCommand::ComputeRewards {
                    epoch,
                    out_filename,
                    dz_ledger_rpc_options,
                } => {
                    println!("ComputeRewards");
                    println!("  epoch: {}", epoch);
                    println!("  out_filename: {:?}", out_filename);
                    println!("  dz_ledger_rpc_options: {:?}", dz_ledger_rpc_options);
                }
                ContributorSubCommand::Configure {
                    service_key,
                    recipient_shares,
                    solana_payer_options,
                } => {
                    println!("Configure");
                    println!("  service_key: {:?}", service_key);
                    println!("  recipient_shares: {:?}", recipient_shares);
                    println!("  solana_payer_options: {:?}", solana_payer_options);
                }
                ContributorSubCommand::Fetch {
                    service_key,
                    solana_rpc_options,
                } => {
                    println!("Fetch");
                    println!("  service_key: {:?}", service_key);
                    println!("  solana_rpc_options: {:?}", solana_rpc_options);
                }
                ContributorSubCommand::FetchByManager {
                    rewards_manager_key,
                    solana_rpc_options,
                } => {
                    println!("FetchByManager");
                    println!("  rewards_manager_key: {}", rewards_manager_key);
                    println!("  solana_rpc_options: {:?}", solana_rpc_options);
                }
                ContributorSubCommand::Initialize {
                    service_key,
                    solana_payer_options,
                } => {
                    println!("Initialize");
                    println!("  service_key: {:?}", service_key);
                    println!("  solana_payer_options: {:?}", solana_payer_options);
                }
            },
            DoubleZero2zSolanaCommand::InitializeJournal {
                solana_payer_options,
            } => {
                println!("InitializeJournal");
                println!("  solana_payer_options: {:?}", solana_payer_options);
            }
            DoubleZero2zSolanaCommand::InitializeProgram {
                solana_payer_options,
            } => {
                println!("InitializeProgram");
                println!("  solana_payer_options: {:?}", solana_payer_options);
            }
            DoubleZero2zSolanaCommand::Prepaid(prepaid) => match prepaid.command {
                PrepaidSubCommand::Initialize {
                    service_key,
                    solana_payer_options,
                } => {
                    println!("Initialize");
                    println!("  service_key: {:?}", service_key);
                    println!("  solana_payer_options: {:?}", solana_payer_options);
                }
                PrepaidSubCommand::Load {
                    service_key,
                    valid_through_epoch,
                    solana_payer_options,
                } => {
                    println!("Load");
                    println!("  service_key: {:?}", service_key);
                    println!("  valid_through_epoch: {:?}", valid_through_epoch);
                    println!("  solana_payer_options: {:?}", solana_payer_options);
                }
            },
            DoubleZero2zSolanaCommand::SetAdmin {
                admin_key,
                solana_payer_options,
            } => {
                println!("SetAdmin");
                println!("  admin_key: {}", admin_key);
                println!("  solana_payer_options: {:?}", solana_payer_options);
            }
            DoubleZero2zSolanaCommand::Validator(validator) => match validator.command {
                ValidatorSubCommand::ComputeRevenue {
                    epoch,
                    out_filename,
                    solana_rpc_options,
                } => {
                    println!("ComputeRevenue");
                    println!("  epoch: {:?}", epoch);
                    println!("  out_filename: {:?}", out_filename);
                    println!("  solana_rpc_options: {:?}", solana_rpc_options);
                }
                ValidatorSubCommand::FetchComputedRevenue {
                    epoch,
                    out_filename,
                    dz_ledger_rpc_options,
                } => {
                    println!("FetchComputedRevenue");
                    println!("  epoch: {:?}", epoch);
                    println!("  out_filename: {:?}", out_filename);
                    println!("  dz_ledger_rpc_options: {:?}", dz_ledger_rpc_options);
                }
                ValidatorSubCommand::PayFee {
                    validator_id,
                    epoch_revenue,
                    rewards_from_file,
                    solana_payer_options,
                } => {
                    println!("PayFee");
                    println!("  validator_id: {:?}", validator_id);
                    println!("  epoch_revenue: {:?}", epoch_revenue);
                    println!("  rewards_from_file: {:?}", rewards_from_file);
                    println!("  solana_payer_options: {:?}", solana_payer_options);
                }
                ValidatorSubCommand::RequestAccess {
                    validator_id,
                    service_key,
                    ed25519_signature,
                    solana_payer_options,
                } => {
                    println!("RequestAccess");
                    println!("  validator_id: {:?}", validator_id);
                    println!("  service_key: {:?}", service_key);
                    println!("  ed25519_signature: {:?}", ed25519_signature);
                    println!("  solana_payer_options: {:?}", solana_payer_options);
                }
            },
        }

        Ok(())
    }
}
