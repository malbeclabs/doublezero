use std::io::Write;

use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::{
    payer::{TransactionOutcome, Wallet},
    squads::{OptionalSquadsArgs, try_write_vault_transaction},
};
use doublezero_solana_sdk::{
    shred_subscription::{
        ID,
        instruction::{
            ShredSubscriptionInstructionData, account::SetValidatorClientRewardsProportionAccounts,
        },
        state::{ValidatorClientRewards, find_validator_client_rewards_address},
    },
    try_build_instruction,
};
use solana_commitment_config::CommitmentConfig;
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{instruction::Instruction, pubkey::Pubkey};

/*
   doublezero-solana shreds validator-client-rewards set-proportion \
       --client-id <ID> --proportion <PERCENT> \
       [--multisig <PUBKEY> [--vault-index <U8>]]

   With --multisig, the Squads vault stands in for the wallet as manager, and
   the instruction is printed as a base58 payload for import into Squads
   rather than signed and sent.
*/

// The limit the direct path sets for the version check and the proportion
// write together, and the figure the vault output tells the operator to budget
// on the Squads execute transaction, which carries no version check and so
// needs less.
const COMPUTE_UNIT_LIMIT: u32 = 35_000;

#[derive(Debug, Args)]
pub struct SetProportionCommand {
    /// Validator client ID.
    #[arg(long)]
    client_id: u16,
    /// Rewards proportion as a percentage (0–100, e.g. 50.5 for 50.5%).
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

impl SetProportionCommand {
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
            "Shred subscription - Set Validator Client Rewards Proportion"
        )?;
        writeln!(
            out,
            "Client ID: {}, Proportion: {}% ({} bps)",
            self.client_id, self.proportion, proportion_bps
        )?;

        let ix = try_build_instruction(
            &ID,
            SetValidatorClientRewardsProportionAccounts::new(&actor_key, self.client_id),
            &ShredSubscriptionInstructionData::SetValidatorClientRewardsProportion(proportion_bps),
        )?;

        match actor {
            Actor::Wallet(wallet) => {
                writeln!(out, "Manager: {actor_key}")?;

                let check_ix = super::super::build_check_cli_version_instruction()?;
                let instructions =
                    direct_instructions(check_ix, ix, wallet.compute_unit_price_ix.as_ref());

                let transaction = wallet.new_transaction(&instructions).await?;
                let tx_outcome = wallet.send_or_simulate_transaction(&transaction).await?;

                if let TransactionOutcome::Executed(tx_sig) = tx_outcome {
                    writeln!(out, "Set validator client rewards proportion: {tx_sig}")?;
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
                    "The payload carries no version check and no compute budget: it executes \
                     days after it is written, and Squads sets the budget on its own execute \
                     transaction. Budget {COMPUTE_UNIT_LIMIT} compute units there."
                )?;
                writeln!(out)?;

                // The payload is the proportion instruction alone. No
                // CheckCliVersion: it is evaluated at execute, days after this
                // payload is stamped, so it says nothing about the version that
                // built the payload and a floor raised while approvals are
                // collected would only revert it. No compute budget instructions
                // either, since Squads sets the budget on its own execute
                // transaction and a budget instruction reached through a CPI is a
                // no-op that only burns compute units. The checked encoder
                // underneath refuses a payload that needs a second signer or
                // will not fit the transaction Squads wraps around it.
                try_write_vault_transaction(out, &connection, &vault_key, &[ix])?;
            }
        }

        Ok(())
    }
}

/// The instruction list the wallet signs: the version check, the proportion
/// write, and the compute budget.
fn direct_instructions(
    check_cli_version_ix: Instruction,
    set_proportion_ix: Instruction,
    compute_unit_price_ix: Option<&Instruction>,
) -> Vec<Instruction> {
    let mut instructions = vec![
        check_cli_version_ix,
        set_proportion_ix,
        ComputeBudgetInstruction::set_compute_unit_limit(COMPUTE_UNIT_LIMIT),
    ];
    instructions.extend(compute_unit_price_ix.cloned());
    instructions
}

fn percentage_to_bps(pct: f64) -> Result<u16> {
    if !(0.0..=100.0).contains(&pct) {
        bail!("Proportion must be between 0 and 100 (got {pct})");
    }
    Ok((pct * 100.0).round() as u16)
}

#[cfg(test)]
mod tests {
    use clap::Parser;
    use doublezero_solana_client_tools::squads::{
        SQUADS_IMPORT_MEMO_RESERVE_BYTES, try_encode_vault_transaction,
        vault_transaction_payload_budget,
    };
    use solana_sdk::message::Message;

    use super::*;

    #[derive(Parser)]
    struct Cli {
        #[command(flatten)]
        cmd: SetProportionCommand,
    }

    fn parse(extra: &[&str]) -> Result<SetProportionCommand, clap::Error> {
        let mut args = vec!["test", "--client-id", "7", "--proportion", "50.5"];
        args.extend_from_slice(extra);
        Cli::try_parse_from(args).map(|cli| cli.cmd)
    }

    fn set_proportion_ix(manager_key: &Pubkey) -> Instruction {
        try_build_instruction(
            &ID,
            SetValidatorClientRewardsProportionAccounts::new(manager_key, 7),
            &ShredSubscriptionInstructionData::SetValidatorClientRewardsProportion(5_050),
        )
        .unwrap()
    }

    #[test]
    fn test_parses_without_multisig() {
        let cmd = parse(&[]).unwrap();
        assert_eq!(cmd.client_id, 7);
        assert_eq!(cmd.proportion, 50.5);
        assert!(cmd.squads.multisig.is_none());
    }

    #[test]
    fn test_parses_multisig_and_vault_index() {
        let multisig = Pubkey::new_unique();
        let cmd = parse(&["--multisig", &multisig.to_string(), "--vault-index", "2"]).unwrap();
        assert_eq!(cmd.squads.multisig, Some(multisig));
        assert_eq!(cmd.squads.vault_index, 2);
    }

    #[test]
    fn test_vault_index_requires_multisig() {
        assert!(parse(&["--vault-index", "2"]).is_err());
    }

    #[test]
    fn test_vault_payload_encodes_through_the_checked_encoder() {
        let vault_key = Pubkey::new_unique();
        let instructions = [set_proportion_ix(&vault_key)];
        try_encode_vault_transaction(&vault_key, &instructions).unwrap();

        let check_cli_version_ix =
            super::super::super::build_check_cli_version_instruction().unwrap();
        assert!(!instructions.contains(&check_cli_version_ix));
        assert!(
            instructions
                .iter()
                .all(|ix| ix.program_id != solana_compute_budget_interface::ID)
        );
    }

    #[test]
    fn test_vault_payload_leaves_room_for_an_import_memo() {
        let vault_key = Pubkey::new_unique();
        let instructions = [set_proportion_ix(&vault_key)];
        let payload = Message::new(&instructions, Some(&vault_key)).serialize();
        let budget = vault_transaction_payload_budget(instructions.len());
        assert!(
            payload.len() + SQUADS_IMPORT_MEMO_RESERVE_BYTES <= budget,
            "{} of {budget}",
            payload.len()
        );
    }

    #[test]
    fn test_direct_instructions_keep_the_version_check_and_budget() {
        let wallet_key = Pubkey::new_unique();
        let check_cli_version_ix =
            super::super::super::build_check_cli_version_instruction().unwrap();
        let price_ix = ComputeBudgetInstruction::set_compute_unit_price(1_000);

        let without_price = direct_instructions(
            check_cli_version_ix.clone(),
            set_proportion_ix(&wallet_key),
            None,
        );
        assert_eq!(without_price.len(), 3);
        assert_eq!(without_price[0], check_cli_version_ix);
        assert_eq!(without_price[1], set_proportion_ix(&wallet_key));
        assert_eq!(
            without_price[2],
            ComputeBudgetInstruction::set_compute_unit_limit(COMPUTE_UNIT_LIMIT)
        );

        let with_price = direct_instructions(
            check_cli_version_ix,
            set_proportion_ix(&wallet_key),
            Some(&price_ix),
        );
        assert_eq!(with_price.len(), 4);
        assert_eq!(with_price[3], price_ix);
    }

    #[test]
    fn test_percentage_to_bps_rounds_and_bounds() {
        assert_eq!(percentage_to_bps(50.5).unwrap(), 5_050);
        assert_eq!(percentage_to_bps(0.0).unwrap(), 0);
        assert_eq!(percentage_to_bps(100.0).unwrap(), 10_000);
        assert_eq!(percentage_to_bps(12.345).unwrap(), 1_235);
        assert!(percentage_to_bps(100.1).is_err());
        assert!(percentage_to_bps(-0.1).is_err());
    }
}
