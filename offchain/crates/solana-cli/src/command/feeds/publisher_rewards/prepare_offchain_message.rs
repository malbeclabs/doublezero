use std::{io::Write, time::Duration};

use anyhow::{Context, Result, ensure};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_solana_client_tools::rpc::SolanaConnectionOptions;
use doublezero_solana_sdk::feed_subscription::{
    ID, types::ConfigureFeedPublisherRewardsAuthMessage,
};
use solana_sdk::pubkey::Pubkey;

use super::rewards_mint_arg::RewardsMintArg;

/*
   doublezero-solana feeds publisher-rewards prepare-offchain-message \
       --node-id <PUBKEY> --rewards-token-owner <WALLET> \
       [--rewards-token-mint <MINT|2z|usdc|wsol>] \
       [--deadline-slot <ABS> | --valid-for <DURATION>] [--json]
*/

const DEFAULT_VALID_FOR: Duration = Duration::from_secs(60 * 60);

// Cluster-independent on purpose, so the deadline a `--valid-for` gives is reproducible.
const NOMINAL_SLOT_DURATION: Duration = Duration::from_millis(350);

#[derive(Debug, Args)]
pub struct PrepareOffchainMessageCommand {
    /// Validator identity that signs the message.
    #[arg(long)]
    node_id: Pubkey,

    /// Mint to be paid in: a base58 pubkey or one of `2z`, `usdc`, `wsol`.
    #[arg(long, default_value = "2z")]
    rewards_token_mint: RewardsMintArg,

    /// Wallet whose associated token account is paid.
    #[arg(long = "rewards-token-owner", value_name = "REWARDS_TOKEN_OWNER")]
    rewards_token_owner_key: Pubkey,

    /// Last slot at which the program accepts the signature.
    #[arg(long, conflicts_with = "valid_for")]
    deadline_slot: Option<u64>,

    /// How long the signature stays valid, such as `1h` or `30m`, converted at a nominal 350 ms
    /// per slot. Defaults to `1h`.
    #[arg(long, value_parser = humantime::parse_duration)]
    valid_for: Option<Duration>,

    /// Print JSON instead of the summary.
    #[arg(long)]
    json: bool,

    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

impl PrepareOffchainMessageCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        ensure!(
            self.node_id != Pubkey::default(),
            "--node-id must not be the default pubkey"
        );
        ensure!(
            self.rewards_token_owner_key != Pubkey::default(),
            "--rewards-token-owner must not be the default pubkey"
        );

        let connection = crate::command::solana_connection(ctx, &self.connection_options);
        let rewards_token_mint_key = self.rewards_token_mint.try_resolve(&connection).await?;
        super::try_check_feed_publishers_reward_token(&connection, &rewards_token_mint_key).await?;

        let current_slot = connection
            .get_slot()
            .await
            .context("reading the current slot")?;
        let deadline_slot = try_deadline_slot(current_slot, self.deadline_slot, self.valid_for)?;

        let hex = ConfigureFeedPublisherRewardsAuthMessage {
            program_id: *ID,
            node_id: self.node_id,
            rewards_token_owner_key: self.rewards_token_owner_key,
            rewards_token_mint_key,
            deadline_slot,
        }
        .to_hex_encoded();
        let hex = std::str::from_utf8(&hex).expect("hex is ASCII");

        if self.json {
            writeln!(
                out,
                "{}",
                serde_json::json!({
                    "hex": hex,
                    "deadline_slot": deadline_slot,
                })
            )?;
        } else {
            writeln!(out, "Hex message:    {hex}")?;
            writeln!(out, "Deadline slot:  {deadline_slot}")?;
            writeln!(out)?;
            writeln!(out, "Sign with:")?;
            writeln!(
                out,
                "  solana sign-offchain-message {hex} --keypair <validator-identity>"
            )?;
            writeln!(out)?;
            writeln!(out, "Then submit:")?;
            writeln!(
                out,
                "  doublezero-solana feeds publisher-rewards configure \\
    --node-id {} --rewards-token-mint {rewards_token_mint_key} --rewards-token-owner {} \\
    --deadline-slot {deadline_slot} --signature <BASE58>",
                self.node_id, self.rewards_token_owner_key
            )?;
        }

        Ok(())
    }
}

/// The deadline slot to sign, refused when the program would refuse it: already passed, or more
/// than `MAX_DEADLINE_SLOTS_AHEAD` past the current slot.
fn try_deadline_slot(
    current_slot: u64,
    deadline_slot: Option<u64>,
    valid_for: Option<Duration>,
) -> Result<u64> {
    let deadline_slot = deadline_slot.unwrap_or_else(|| {
        let valid_for = valid_for.unwrap_or(DEFAULT_VALID_FOR);
        let slots = u64::try_from(valid_for.as_millis() / NOMINAL_SLOT_DURATION.as_millis())
            .unwrap_or(u64::MAX);
        current_slot.saturating_add(slots)
    });
    let accepted_deadline_slots = current_slot
        ..=current_slot
            .saturating_add(ConfigureFeedPublisherRewardsAuthMessage::MAX_DEADLINE_SLOTS_AHEAD);
    ensure!(
        accepted_deadline_slots.contains(&deadline_slot),
        "deadline slot {deadline_slot} is outside {}..={}, the range the feed program accepts at \
         the current slot {current_slot}",
        accepted_deadline_slots.start(),
        accepted_deadline_slots.end()
    );
    Ok(deadline_slot)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_deadline_slot_defaults_to_an_hour_of_nominal_slots() {
        // 3,600,000 ms / 350 ms = 10,285 slots, truncated.
        assert_eq!(try_deadline_slot(100, None, None).unwrap(), 100 + 10_285);
        // 1,800,000 ms / 350 ms = 5,142 slots, truncated.
        assert_eq!(
            try_deadline_slot(100, None, Some(Duration::from_secs(30 * 60))).unwrap(),
            100 + 5_142
        );
        assert_eq!(try_deadline_slot(100, Some(500), None).unwrap(), 500);
    }

    #[test]
    fn test_deadline_slot_refuses_what_the_program_refuses() {
        assert_eq!(try_deadline_slot(1_000, Some(1_000), None).unwrap(), 1_000);
        assert_eq!(
            try_deadline_slot(1_000, Some(1_000 + 216_000), None).unwrap(),
            217_000
        );

        for (deadline_slot, valid_for, refused_deadline_slot) in [
            (Some(999), None, 999),
            (Some(217_001), None, 217_001),
            // A day of nominal slots is 246,857, past the program's 216,000.
            (
                None,
                Some(Duration::from_secs(24 * 60 * 60)),
                1_000 + 246_857,
            ),
        ] {
            assert_eq!(
                try_deadline_slot(1_000, deadline_slot, valid_for)
                    .unwrap_err()
                    .to_string(),
                format!(
                    "deadline slot {refused_deadline_slot} is outside 1000..=217000, the range \
                     the feed program accepts at the current slot 1000"
                )
            );
        }
    }
}
