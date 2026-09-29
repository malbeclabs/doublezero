use std::io::Write;

use anyhow::{Context, Result, ensure};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::{
    payer::{TransactionOutcome, Wallet},
    squads::{OptionalSquadsArgs, try_write_vault_transaction},
};
use doublezero_solana_sdk::{
    feed_subscription::{
        ID,
        instruction::{
            FeedSubscriptionInstructionData,
            account::ConfigureValidatorClientRewardsProportionAccounts,
        },
        state::{
            MAX_VALIDATOR_CLIENT_REWARDS_PROPORTION_BPS, ProgramConfig, ValidatorClientRewards,
            find_program_config_address, find_validator_client_rewards_address,
        },
    },
    try_build_instruction,
};
use solana_commitment_config::CommitmentConfig;
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{instruction::Instruction, pubkey::Pubkey};

/*
   doublezero-solana feeds validator-client-rewards configure-proportion \
       --client-id <ID> --proportion <PERCENT> \
       [--multisig <PUBKEY> [--vault-index <U8>]]

   With --multisig, the Squads vault stands in for the wallet as manager, and
   the instruction is printed as a base58 payload for import into Squads
   rather than signed and sent.
*/

// The limit the direct path sets, and the figure the vault output tells the
// operator to budget on the Squads execute transaction.
const COMPUTE_UNIT_LIMIT: u32 = 20_000;

#[derive(Debug, Args)]
pub struct ConfigureProportionCommand {
    /// Validator client ID.
    #[arg(long)]
    client_id: u16,
    /// Rewards proportion as a percentage (0-35, e.g. 12.5 for 12.5%).
    #[arg(long)]
    proportion: f64,
    #[command(flatten)]
    squads: OptionalSquadsArgs,
    #[command(flatten)]
    write_opts: crate::command::WriteVerbOptions,
}

// Who signs for the manager: the wallet itself, or a Squads vault the multisig
// signs for once the payload is imported and approved.
enum Actor {
    Wallet(Box<Wallet>),
    Vault(Pubkey),
}

impl Actor {
    fn key(&self) -> Pubkey {
        match self {
            Self::Wallet(wallet) => wallet.pubkey(),
            Self::Vault(vault_key) => *vault_key,
        }
    }

    fn label(&self) -> &'static str {
        match self {
            Self::Wallet(_) => "wallet",
            Self::Vault(_) => "vault",
        }
    }
}

impl ConfigureProportionCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let proportion_bps = percentage_to_bps(self.proportion)?;

        // The vault path builds no wallet, so --multisig runs on a machine with
        // no keypair. Every signing and sending option is inert there. Both
        // paths read through this connection, which resolves the URL the same
        // way the wallet does.
        let connection =
            crate::command::solana_connection(ctx, &self.write_opts.connection_options);
        let actor = match self.squads.try_find_vault_address(&connection).await? {
            Some(vault_key) => Actor::Vault(vault_key),
            None => Actor::Wallet(Box::new(crate::command::build_wallet(
                ctx,
                self.write_opts,
            )?)),
        };
        let actor_key = actor.key();

        // The program refuses this instruction while paused. Stopping here saves a payload the
        // multisig could never execute from using up an approval round.
        let program_config_key = find_program_config_address().0;
        let program_config = connection
            .try_fetch_zero_copy_data_with_commitment::<ProgramConfig>(
                &program_config_key,
                CommitmentConfig::confirmed(),
            )
            .await
            .with_context(|| {
                format!("fetching the feed subscription program config (PDA {program_config_key})")
            })?;
        ensure!(
            !program_config.is_paused(),
            "the feed subscription program is paused, so it refuses this"
        );

        let validator_client_rewards_key = find_validator_client_rewards_address(self.client_id).0;
        let validator_client_rewards = connection
            .try_fetch_zero_copy_data_with_commitment::<ValidatorClientRewards>(
                &validator_client_rewards_key,
                CommitmentConfig::confirmed(),
            )
            .await
            .with_context(|| {
                format!(
                    "fetching validator client rewards for client-id {} (PDA {validator_client_rewards_key})",
                    self.client_id
                )
            })?;
        // Refusing here is what stops a payload the multisig can never execute
        // from consuming an approval round, and what gives a wallet that is not
        // the manager a message naming the mismatch rather than the program's
        // invalid account data.
        super::validate_manager(
            actor.label(),
            &actor_key,
            &validator_client_rewards.manager_key,
        )?;

        writeln!(
            out,
            "Feed subscription - Configure Validator Client Rewards Proportion"
        )?;
        writeln!(
            out,
            "Client ID: {}, Proportion: {}% ({} bps)",
            self.client_id, self.proportion, proportion_bps
        )?;

        let ix = try_build_instruction(
            &ID,
            ConfigureValidatorClientRewardsProportionAccounts::new(&actor_key, self.client_id),
            &FeedSubscriptionInstructionData::ConfigureValidatorClientRewardsProportion(
                proportion_bps,
            ),
        )?;

        match actor {
            Actor::Wallet(wallet) => {
                writeln!(out, "Manager: {actor_key}")?;

                let instructions = direct_instructions(ix, wallet.compute_unit_price_ix.as_ref());

                let transaction = wallet.new_transaction(&instructions).await?;
                let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

                if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
                    writeln!(
                        out,
                        "Configured validator client rewards proportion: {tx_sig}"
                    )?;
                    wallet.write_verbose_output(out, &[tx_sig]).await?;
                }
            }
            Actor::Vault(vault_key) => {
                writeln!(out, "Manager (vault): {vault_key}")?;
                if let Some(multisig_key) = self.squads.multisig {
                    writeln!(
                        out,
                        "Multisig: {multisig_key} (vault index {})",
                        self.squads.vault_index
                    )?;
                }
                writeln!(out)?;
                writeln!(
                    out,
                    "Nothing was signed or sent. --keypair, --fee-payer, --dry-run, \
                     --with-compute-unit-price and --verbose have no effect with --multisig."
                )?;
                writeln!(
                    out,
                    "The payload carries no compute budget, since Squads sets the budget on its \
                     own execute transaction. Budget {COMPUTE_UNIT_LIMIT} compute units there."
                )?;
                writeln!(out)?;

                // The checked encoder underneath refuses a payload that needs
                // a second signer or will not fit the transaction Squads wraps
                // around it.
                try_write_vault_transaction(out, &connection, &vault_key, &vault_instructions(ix))?;
            }
        }

        Ok(())
    }
}

/// The instruction list the wallet signs: the proportion write and the
/// compute budget.
fn direct_instructions(
    configure_proportion_ix: Instruction,
    compute_unit_price_ix: Option<&Instruction>,
) -> Vec<Instruction> {
    let mut instructions = vec![
        configure_proportion_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(COMPUTE_UNIT_LIMIT),
    ];
    instructions.extend(compute_unit_price_ix.cloned());
    instructions
}

/// The payload a vault imports: the proportion instruction alone.
///
/// No compute budget instruction, since Squads sets the budget on its own
/// execute transaction and a budget instruction reached through a CPI is a
/// no-op that only burns compute units.
fn vault_instructions(configure_proportion_ix: Instruction) -> Vec<Instruction> {
    vec![configure_proportion_ix]
}

fn percentage_to_bps(pct: f64) -> Result<u16> {
    let max_pct = f64::from(MAX_VALIDATOR_CLIENT_REWARDS_PROPORTION_BPS) / 100.0;
    ensure!(
        (0.0..=max_pct).contains(&pct),
        "Proportion must be between 0 and {max_pct} (got {pct})"
    );
    Ok((pct * 100.0).round() as u16)
}

#[cfg(test)]
mod tests {
    use clap::{Parser, error::ErrorKind};
    use doublezero_solana_client_tools::squads::{
        SQUADS_IMPORT_MEMO_RESERVE_BYTES, try_encode_vault_transaction,
        vault_transaction_payload_budget,
    };
    use solana_sdk::message::Message;

    use super::*;

    #[derive(Parser)]
    struct Cli {
        #[command(flatten)]
        cmd: ConfigureProportionCommand,
    }

    fn parse(extra: &[&str]) -> Result<ConfigureProportionCommand, clap::Error> {
        let mut args = vec!["test", "--client-id", "7", "--proportion", "12.5"];
        args.extend_from_slice(extra);
        Cli::try_parse_from(args).map(|cli| cli.cmd)
    }

    fn configure_proportion_ix(manager_key: &Pubkey) -> Instruction {
        try_build_instruction(
            &ID,
            ConfigureValidatorClientRewardsProportionAccounts::new(manager_key, 7),
            &FeedSubscriptionInstructionData::ConfigureValidatorClientRewardsProportion(1_250),
        )
        .unwrap()
    }

    #[test]
    fn test_parses_without_multisig() {
        let cmd = parse(&[]).unwrap();
        assert_eq!(cmd.client_id, 7);
        assert_eq!(cmd.proportion, 12.5);
        assert!(cmd.squads.multisig.is_none());
    }

    #[test]
    fn test_parses_multisig_and_vault_index() {
        let multisig_key = Pubkey::new_unique();
        let cmd = parse(&[
            "--multisig",
            &multisig_key.to_string(),
            "--vault-index",
            "2",
        ])
        .unwrap();
        assert_eq!(cmd.squads.multisig, Some(multisig_key));
        assert_eq!(cmd.squads.vault_index, 2);
    }

    #[test]
    fn test_vault_index_requires_multisig() {
        let error = parse(&["--vault-index", "2"]).unwrap_err();
        assert_eq!(error.kind(), ErrorKind::MissingRequiredArgument);
    }

    #[test]
    fn test_vault_payload_encodes_through_the_checked_encoder() {
        let vault_key = Pubkey::new_unique();
        let instructions = vault_instructions(configure_proportion_ix(&vault_key));
        try_encode_vault_transaction(&vault_key, &instructions).unwrap();

        assert_eq!(instructions.len(), 1);
        assert!(
            instructions
                .iter()
                .all(|ix| ix.program_id != solana_compute_budget_interface::ID)
        );
    }

    #[test]
    fn test_vault_payload_leaves_room_for_an_import_memo() {
        let vault_key = Pubkey::new_unique();
        let instructions = vault_instructions(configure_proportion_ix(&vault_key));
        let payload = Message::new(&instructions, Some(&vault_key)).serialize();
        let budget = vault_transaction_payload_budget(instructions.len());
        assert!(
            payload.len() + SQUADS_IMPORT_MEMO_RESERVE_BYTES <= budget,
            "{} of {budget}",
            payload.len()
        );
    }

    #[test]
    fn test_direct_instructions_keep_the_budget() {
        let wallet_key = Pubkey::new_unique();
        let price_ix = ComputeBudgetInstruction::set_compute_unit_price(1_000);

        let without_price = direct_instructions(configure_proportion_ix(&wallet_key), None);
        assert_eq!(without_price.len(), 2);
        assert_eq!(without_price[0], configure_proportion_ix(&wallet_key));
        assert_eq!(
            without_price[1],
            ComputeBudgetInstruction::set_compute_unit_limit(COMPUTE_UNIT_LIMIT)
        );

        let with_price = direct_instructions(configure_proportion_ix(&wallet_key), Some(&price_ix));
        assert_eq!(with_price.len(), 3);
        assert_eq!(with_price[2], price_ix);
    }

    #[test]
    fn test_percentage_to_bps_rounds_and_bounds() {
        assert_eq!(percentage_to_bps(12.5).unwrap(), 1_250);
        assert_eq!(percentage_to_bps(0.0).unwrap(), 0);
        assert_eq!(percentage_to_bps(35.0).unwrap(), 3_500);
        assert_eq!(percentage_to_bps(12.345).unwrap(), 1_235);
        assert_eq!(
            percentage_to_bps(35.01).unwrap_err().to_string(),
            "Proportion must be between 0 and 35 (got 35.01)"
        );
        assert_eq!(
            percentage_to_bps(-0.1).unwrap_err().to_string(),
            "Proportion must be between 0 and 35 (got -0.1)"
        );
    }
}
