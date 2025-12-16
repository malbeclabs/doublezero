use anyhow::{Result, bail};
use clap::Args;
use doublezero_solana_client_tools::{
    account::zero_copy::ZeroCopyAccountOwnedData,
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::{
    PrecomputedDiscriminator,
    revenue_distribution::{self, state::SolanaValidatorDeposit},
};
use solana_account_decoder_client_types::UiAccountEncoding;
use solana_client::{
    rpc_config::{RpcAccountInfoConfig, RpcProgramAccountsConfig},
    rpc_filter::{Memcmp, RpcFilterType},
};
use solana_sdk::pubkey::Pubkey;

use crate::command::revenue_distribution::try_fetch_solana_validator_deposit;

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
    balance: String,
    written_off_debt: String,
}

impl ValidatorDepositsCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        let Self {
            node_id,
            balance_only,
            connection_options,
        } = self;

        let connection = SolanaConnection::from(connection_options);

        let (outputs, fund_warning_message) = if let Some(node_id) = node_id {
            let (deposit_key, deposit, deposit_balance) =
                try_fetch_solana_validator_deposit(&connection, &node_id).await?;

            if let Some(deposit) = deposit {
                if balance_only {
                    println!("{:.9}", deposit_balance as f64 * 1e-9);

                    return Ok(());
                }

                (
                    vec![ValidatorDepositsTableRow {
                        deposit_pda: deposit_key,
                        node_id: deposit.node_id,
                        balance: format!("{:.9} SOL", deposit_balance as f64 * 1e-9),
                        written_off_debt: if deposit.written_off_sol_debt == 0 {
                            Default::default()
                        } else {
                            format!("{:.9} SOL", deposit.written_off_sol_debt as f64 * 1e-9)
                        },
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
                        balance: format!("{:.9} SOL", deposit_balance as f64 * 1e-9),
                        written_off_debt: Default::default(),
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

            let rent_sysvar = connection
                .try_fetch_sysvar::<solana_sdk::rent::Rent>()
                .await?;

            let mut outputs = connection
                .get_program_accounts_with_config(&revenue_distribution::ID, config)
                .await?
                .into_iter()
                .map(|(deposit_key, deposit_account_info)| {
                    let balance = doublezero_solana_client_tools::account::balance(
                        &deposit_account_info,
                        &rent_sysvar,
                    );
                    let deposit_account =
                        ZeroCopyAccountOwnedData::<SolanaValidatorDeposit>::from_account(
                            &deposit_account_info,
                        )
                        .unwrap();

                    ValidatorDepositsTableRow {
                        deposit_pda: deposit_key,
                        node_id: deposit_account.node_id,
                        balance: format!("{:.9} SOL", balance as f64 * 1e-9),
                        written_off_debt: if deposit_account.written_off_sol_debt == 0 {
                            Default::default()
                        } else {
                            format!(
                                "{:.9} SOL",
                                deposit_account.written_off_sol_debt as f64 * 1e-9
                            )
                        },
                    }
                })
                .collect::<Vec<_>>();

            outputs.sort_by_key(|row| row.node_id.to_string());

            (outputs, None)
        };

        super::print_table(
            outputs,
            super::TableOptions {
                columns_aligned_right: Some(&[2, 3]),
            },
        );

        if let Some(fund_warning_message) = fund_warning_message {
            println!("{fund_warning_message}");
            println!();
        }

        Ok(())
    }
}
