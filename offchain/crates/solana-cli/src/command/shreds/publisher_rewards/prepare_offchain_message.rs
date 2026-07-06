use std::time::Duration;

use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_solana_client_tools::rpc::{SolanaConnection, SolanaConnectionOptions};
use doublezero_solana_sdk::{
    Pubkey,
    shred_subscription::{
        ID, state::find_shred_reward_token_address,
        types::ConfigureValidatorPublisherRewardsAuthMessage,
    },
};

use super::rewards_mint_arg::RewardsMintArg;

/*
   doublezero-solana shreds publisher-rewards prepare-offchain-message \
       --node-id <PUBKEY> --rewards-token-owner <OWNER> \
       [--rewards-token-mint <MINT|2z|usdc|wsol>] \
       [--deadline-slot <ABS> | --valid-for <DURATION>] [--json]
*/

const SLOT_DURATION_MS: u64 = 400;
const DEFAULT_VALID_FOR: Duration = Duration::from_secs(60 * 60);

#[derive(Debug, Args)]
pub struct PrepareOffchainMessageCommand {
    #[arg(long)]
    pub node_id: Pubkey,
    /// Mint to receive rewards in. Accepts a base58 pubkey or one of the
    /// aliases `2z`, `usdc`, `wsol` (env-aware where applicable). Defaults
    /// to `2z`.
    #[arg(long, default_value = "2z")]
    pub rewards_token_mint: RewardsMintArg,
    #[arg(long)]
    pub rewards_token_owner: Pubkey,

    /// Absolute slot after which the authorization is no longer valid.
    /// Mutually exclusive with `--valid-for`.
    #[arg(long, conflicts_with = "valid_for")]
    pub deadline_slot: Option<u64>,

    /// Duration the authorization remains valid (e.g. `1h`, `30m`, `7200s`).
    /// Default: `1h`. Parsed via `humantime`.
    #[arg(long, value_parser = parse_valid_for)]
    pub valid_for: Option<Duration>,

    /// Emit machine-readable JSON instead of the human-friendly multi-line
    /// summary. Useful for shell pipelines.
    #[arg(long)]
    pub json: bool,

    #[command(flatten)]
    pub connection_options: SolanaConnectionOptions,
}

impl PrepareOffchainMessageCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        if self.node_id == Pubkey::default() {
            bail!("--node-id must not be the default pubkey");
        }
        if self.rewards_token_owner == Pubkey::default() {
            bail!("--rewards-token-owner must not be the default pubkey");
        }

        let connection = SolanaConnection::from(self.connection_options);
        let rewards_token_mint = self.rewards_token_mint.resolve(&connection).await?;

        // Pre-flight: the on-chain `configure` rejects unregistered/disabled
        // mints. Catching it here saves a full offline round-trip (hex →
        // validator-host signing → back → configure submit) for a mint that
        // would never succeed.
        let srt_pda = find_shred_reward_token_address(&rewards_token_mint).0;
        let srt_account = connection
            .0
            .get_account_with_commitment(&srt_pda, connection.0.commitment())
            .await
            .with_context(|| {
                format!("failed to read ShredRewardToken account at {srt_pda} for pre-flight")
            })?
            .value;
        super::validate_shred_reward_token(&rewards_token_mint, &srt_pda, srt_account.as_ref())?;

        let current_slot = connection
            .get_slot()
            .await
            .context("failed to query current slot from RPC")?;

        let deadline_slot =
            resolve_deadline_slot(current_slot, self.deadline_slot, self.valid_for)?;

        let message = ConfigureValidatorPublisherRewardsAuthMessage {
            program_id: *ID,
            node_id: self.node_id,
            rewards_token_owner_key: self.rewards_token_owner,
            rewards_token_mint_key: rewards_token_mint,
            deadline_slot,
        };

        let hex = std::str::from_utf8(&message.to_hex_encoded())
            .expect("hex output is ASCII")
            .to_owned();

        if self.json {
            println!(
                "{}",
                serde_json::json!({
                    "hex": hex,
                    "deadline_slot": deadline_slot,
                })
            );
        } else {
            println!("Hex message:    {hex}");
            println!("Deadline slot:  {deadline_slot}");
            println!();
            println!("Sign with:");
            println!("  solana sign-offchain-message {hex} --keypair <validator-identity>");
            println!();
            println!("Then submit:");
            println!(
                "  doublezero-solana shreds publisher-rewards configure \\
    --node-id {} --rewards-token-mint {rewards_token_mint} --rewards-token-owner {} \\
    --deadline-slot {deadline_slot} --signature <BASE58>",
                self.node_id, self.rewards_token_owner
            );
        }

        Ok(())
    }
}

/// Pure helper: resolve the absolute deadline slot from CLI inputs.
///
/// `--deadline-slot` always wins. If absent, `--valid-for` is divided by the
/// nominal slot duration (400ms) and added to `current_slot`. If both are
/// `None`, defaults to 1h. Both supplied is an error.
pub(crate) fn resolve_deadline_slot(
    current_slot: u64,
    deadline_slot: Option<u64>,
    valid_for: Option<Duration>,
) -> Result<u64> {
    if deadline_slot.is_some() && valid_for.is_some() {
        // Defense in depth — clap also catches this via `conflicts_with`.
        bail!("--deadline-slot and --valid-for are mutually exclusive");
    }
    if let Some(d) = deadline_slot {
        return Ok(d);
    }
    let duration = valid_for.unwrap_or(DEFAULT_VALID_FOR);
    let slots = duration
        .as_millis()
        .checked_div(SLOT_DURATION_MS as u128)
        .context("invalid slot duration")?;
    let slots: u64 = slots
        .try_into()
        .context("--valid-for too large to encode as a slot delta")?;
    Ok(current_slot.saturating_add(slots))
}

/// Parse `--valid-for` via `humantime` and reject a zero duration. A zero
/// duration would put `deadline_slot` at the current slot, so the
/// authorization would be born already-expired.
fn parse_valid_for(s: &str) -> Result<Duration, String> {
    let duration = humantime::parse_duration(s).map_err(|e| format!("invalid duration: {e}"))?;
    if duration.is_zero() {
        return Err("--valid-for must be greater than zero".to_owned());
    }
    Ok(duration)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn explicit_deadline_slot_wins() {
        let resolved = resolve_deadline_slot(100, Some(500), None).unwrap();
        assert_eq!(resolved, 500);
    }

    #[test]
    fn valid_for_default_one_hour() {
        let resolved = resolve_deadline_slot(100, None, None).unwrap();
        assert_eq!(resolved, 100 + 9_000);
    }

    #[test]
    fn valid_for_explicit_30m() {
        let resolved =
            resolve_deadline_slot(100, None, Some(Duration::from_secs(30 * 60))).unwrap();
        assert_eq!(resolved, 100 + 4_500);
    }

    #[test]
    fn explicit_and_valid_for_is_error() {
        let r = resolve_deadline_slot(100, Some(500), Some(Duration::from_secs(60)));
        assert!(r.is_err());
    }

    #[test]
    fn parse_valid_for_examples() {
        assert_eq!(parse_valid_for("60s").unwrap(), Duration::from_secs(60));
        assert_eq!(parse_valid_for("5m").unwrap(), Duration::from_secs(300));
        assert_eq!(parse_valid_for("2h").unwrap(), Duration::from_secs(7200));
        // humantime accepts whitespace between number and unit (the
        // hand-rolled parser previously rejected this).
        assert_eq!(parse_valid_for("1 h").unwrap(), Duration::from_secs(3600));
        assert!(parse_valid_for("5x").is_err());
        assert!(parse_valid_for("hello").is_err());
    }

    #[test]
    fn parse_valid_for_zero_rejected() {
        assert!(parse_valid_for("0s").is_err());
        assert!(parse_valid_for("0m").is_err());
    }

    #[test]
    fn parse_valid_for_overflow_rejected() {
        // u64::MAX seconds would overflow the old hand-rolled `num * 60 * 60`.
        // humantime rejects values that don't fit in a `Duration`.
        assert!(parse_valid_for("99999999999999999999h").is_err());
    }
}
