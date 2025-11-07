use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_program_tools::{PrecomputedDiscriminator, zero_copy};
use doublezero_revenue_distribution::state::SolanaValidatorDeposit;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::pubkey::Pubkey;

use crate::command::revenue_distribution::fetch_solana_validator_deposit;

#[derive(Debug, Args)]
pub struct ValidatorDepositsCommand {
    #[arg(long, short = 'n', value_name = "PUBKEY")]
    node_id: Option<Pubkey>,

    /// Can only be used with --node-id.
    #[arg(long, short = 'b')]
    balance_only: bool,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

#[derive(Debug, tabled::Tabled)]
struct ValidatorDepositsTableRow {
    deposit_pda: Pubkey,
    node_id: Pubkey,
    amount: String,
}

impl ValidatorDepositsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            node_id,
            balance_only,
            connection_options,
        } = self;

        let connection = SolanaConnection::try_from(connection_options)?;

        let (mut outputs, fund_warning_message) = if let Some(node_id) = node_id {
            let (deposit_key, deposit, deposit_balance) =
                fetch_solana_validator_deposit(&connection, &node_id).await;

            if let Some(deposit) = deposit {
                if balance_only {
                    println!("{:.9}", deposit_balance as f64 * 1e-9);

                    return Ok(());
                }

                (
                    vec![ValidatorDepositsTableRow {
                        deposit_pda: deposit_key,
                        node_id: deposit.node_id,
                        amount: format!("{:.9}", deposit_balance as f64 * 1e-9),
                    }],
                    None,
                )
            } else if deposit_balance != 0 {
                let warning_message = format!(
                    "⚠️  Warning: Please use \"doublezero-solana revenue-distribution validator-deposit --node-id {node_id} -i\" to create {deposit_key}"
                );

                if balance_only {
                    println!("{:.9}", deposit_balance as f64 * 1e-9);
                    eprintln!();
                    eprintln!("{warning_message}");

                    return Ok(());
                }

                (
                    vec![ValidatorDepositsTableRow {
                        deposit_pda: deposit_key,
                        node_id,
                        amount: format!("{:.9}", deposit_balance as f64 * 1e-9),
                    }],
                    Some(warning_message),
                )
            } else {
                bail!(
                    "No deposit account found at {deposit_key}. Please use \"doublezero-solana revenue-distribution validator-deposit --node-id {node_id} --fund <AMOUNT>\" to deposit SOL"
                );
            }
        } else {
            if balance_only {
                bail!("Cannot use --balance-only without specifying --node-id");
            }

            let config = RpcProgramAccountsConfig {
                filters: Some(vec![RpcFilterType::Memcmp(Memcmp::new_raw_bytes(
                    0,
                    SolanaValidatorDeposit::discriminator_slice().to_vec(),
                ))]),
                account_config: RpcAccountInfoConfig {
                    encoding: Some(UiAccountEncoding::Base64),
                    ..Default::default()
                },
                ..Default::default()
            };

            let accounts = connection
                .get_program_accounts_with_config(&doublezero_revenue_distribution::ID, config)
                .await?;

            let rent_exemption = connection
                .rpc_client
                .get_minimum_balance_for_rent_exemption(
                    zero_copy::data_end::<SolanaValidatorDeposit>(),
                )
                .await?;

            let mut outputs = Vec::with_capacity(accounts.len());
            for (pubkey, account) in accounts {
                let balance = account.lamports.saturating_sub(rent_exemption);
                let (account, _) = zero_copy::checked_from_bytes_with_discriminator::<
                    SolanaValidatorDeposit,
                >(&account.data)
                .context("Failed to deserialize Solana validator deposit")?;
                outputs.push(ValidatorDepositsTableRow {
                    deposit_pda: pubkey,
                    node_id: account.node_id,
                    amount: format!("{:.9}", balance as f64 * 1e-9),
                });
            }

            (outputs, None)
        };

        outputs.sort_by_key(|deposit| (deposit.node_id, deposit.deposit_pda));

        super::print_table(outputs, Default::default());

        if let Some(fund_warning_message) = fund_warning_message {
            println!("{fund_warning_message}");
            println!();
        }

        Ok(())
    }
}
