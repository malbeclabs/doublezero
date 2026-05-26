use anyhow::{Context, Result, bail};
use clap::Args;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, Wallet};
use doublezero_solana_sdk::shred_subscription::state::{
    ValidatorClientRewardsInfo, find_claim_holding_address, find_validator_client_rewards_address,
    parse_validator_client_rewards,
};
use solana_sdk::{commitment_config::CommitmentConfig, program_pack::Pack, pubkey::Pubkey};
use spl_associated_token_account_interface::address::get_associated_token_address;

/*
   doublezero-solana shreds validator-client-rewards show \
       --client-id <ID> [--rewards-token-mint <PUBKEY>] \
       [--subscription-epoch <EPOCH> ...]
*/

#[derive(Debug, Args)]
pub struct ShowCommand {
    /// Validator client ID.
    #[arg(long)]
    pub client_id: u16,
    /// Filter to a specific token mint when listing holdings.
    #[arg(long)]
    pub rewards_token_mint: Option<Pubkey>,
    /// One or more subscription epochs to inspect. Requires --rewards-token-mint.
    #[arg(long = "subscription-epoch", num_args = 0..)]
    pub subscription_epochs: Vec<u64>,
    #[command(flatten)]
    pub solana_payer_options: SolanaPayerOptions,
}

pub(crate) fn render_vcr_summary(
    client_id: u16,
    vcr_key: &Pubkey,
    info: &ValidatorClientRewardsInfo,
) -> String {
    let mut out = String::new();
    out.push_str(&format!(
        "Validator client rewards (client_id={client_id})\n"
    ));
    out.push_str(&format!("  PDA                 : {vcr_key}\n"));
    out.push_str(&format!("  manager             : {}\n", info.manager_key));
    out.push_str(&format!(
        "  description         : {}\n",
        info.short_description.as_deref().unwrap_or("(none)")
    ));
    out.push_str(&format!(
        "  claim holding count : {}\n",
        info.claim_holding_count
    ));
    out
}

/// Status of a token-bearing account (manager ATA or per-epoch holding PDA)
/// for display purposes. Splits the cases that `Option<u64>` previously
/// collapsed so the user can tell apart "wasn't created" from "wrong owner /
/// malformed".
pub(crate) enum TokenAccountStatus {
    Balance(u64),
    DoesNotExist,
    WrongOwner(Pubkey),
    Malformed,
    WrongMint(Pubkey),
}

impl TokenAccountStatus {
    fn render_tail(&self, expected_mint: Option<&Pubkey>) -> String {
        match self {
            TokenAccountStatus::Balance(amt) => format!("balance={amt}"),
            TokenAccountStatus::DoesNotExist => "(does not exist)".to_string(),
            TokenAccountStatus::WrongOwner(owner) => format!("(wrong owner: {owner})"),
            TokenAccountStatus::Malformed => "(malformed token account)".to_string(),
            TokenAccountStatus::WrongMint(found) => match expected_mint {
                Some(expected) => {
                    format!("(wrong mint: found {found}, expected {expected})")
                }
                None => format!("(wrong mint: found {found})"),
            },
        }
    }
}

/// Classify a fetched token account against the expected mint (when known).
pub(crate) fn classify_token_account(
    account: Option<&solana_sdk::account::Account>,
    expected_mint: Option<&Pubkey>,
) -> TokenAccountStatus {
    let Some(account) = account else {
        return TokenAccountStatus::DoesNotExist;
    };
    if account.owner != spl_token_interface::ID {
        return TokenAccountStatus::WrongOwner(account.owner);
    }
    match spl_token_interface::state::Account::unpack(&account.data) {
        Err(_) => TokenAccountStatus::Malformed,
        Ok(token) => match expected_mint {
            Some(expected) if token.mint != *expected => TokenAccountStatus::WrongMint(token.mint),
            _ => TokenAccountStatus::Balance(token.amount),
        },
    }
}

// Format is grep'd by sh/test_doublezero_solana_fork.sh — keep
// "  epoch <num>  <pda>  balance=<n>" stable or update the grep.
pub(crate) fn render_holding_row(
    epoch: u64,
    holding_key: &Pubkey,
    status: &TokenAccountStatus,
    expected_mint: Option<&Pubkey>,
) -> String {
    format!(
        "  epoch {epoch:>5}  {holding_key}  {}",
        status.render_tail(expected_mint)
    )
}

pub(crate) fn render_manager_ata_row(
    ata: &Pubkey,
    status: &TokenAccountStatus,
    expected_mint: Option<&Pubkey>,
) -> String {
    format!(
        "  manager ATA  {ata}  {}",
        status.render_tail(expected_mint)
    )
}

impl ShowCommand {
    pub async fn try_into_execute(self) -> Result<()> {
        if !self.subscription_epochs.is_empty() && self.rewards_token_mint.is_none() {
            bail!("--subscription-epoch requires --rewards-token-mint");
        }

        let dz_connection = self
            .solana_payer_options
            .connection_options
            .clone()
            .into_shred_subscription_connection();
        let wallet = Wallet::try_new(self.solana_payer_options, Some(dz_connection))?;

        let vcr_key = find_validator_client_rewards_address(self.client_id).0;
        let vcr_account = wallet
            .connection
            .get_account_with_commitment(&vcr_key, CommitmentConfig::confirmed())
            .await
            .with_context(|| format!("fetching VCR PDA {vcr_key}"))?
            .value;
        let vcr_data = match vcr_account {
            Some(acct) => acct.data,
            None => {
                println!(
                    "Validator client rewards not initialized for client-id {} (PDA {vcr_key})",
                    self.client_id
                );
                return Ok(());
            }
        };
        let info = parse_validator_client_rewards(&vcr_data)
            .with_context(|| format!("failed to parse ValidatorClientRewards at {vcr_key}"))?;
        print!("{}", render_vcr_summary(self.client_id, &vcr_key, &info));

        // When a mint is supplied, always print the manager's ATA address and
        // balance. Per-epoch holding rows are only listed when the user also
        // supplies one or more `--subscription-epoch` values.
        if let Some(mint) = self.rewards_token_mint {
            println!("Claim holdings for mint {mint}:");
            let manager_ata = get_associated_token_address(&info.manager_key, &mint);
            let ata_account = wallet
                .connection
                .get_account_with_commitment(&manager_ata, CommitmentConfig::confirmed())
                .await
                .with_context(|| format!("fetching manager ATA {manager_ata}"))?
                .value;
            let ata_status = classify_token_account(ata_account.as_ref(), Some(&mint));
            println!(
                "{}",
                render_manager_ata_row(&manager_ata, &ata_status, Some(&mint))
            );

            if !self.subscription_epochs.is_empty() {
                let holding_keys: Vec<Pubkey> = self
                    .subscription_epochs
                    .iter()
                    .map(|e| find_claim_holding_address(&vcr_key, *e, &mint).0)
                    .collect();
                let holding_accounts = wallet
                    .connection
                    .get_multiple_accounts(&holding_keys)
                    .await
                    .with_context(|| "fetching claim holdings")?;
                for ((epoch, key), maybe_acct) in self
                    .subscription_epochs
                    .iter()
                    .zip(holding_keys.iter())
                    .zip(holding_accounts.into_iter())
                {
                    let status = classify_token_account(maybe_acct.as_ref(), Some(&mint));
                    println!("{}", render_holding_row(*epoch, key, &status, Some(&mint)));
                }
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use clap::Parser;

    use super::*;

    #[derive(Parser)]
    struct Cli {
        #[command(flatten)]
        cmd: ShowCommand,
    }

    #[test]
    fn parses_minimum_args() {
        let cli = Cli::try_parse_from(["test", "--client-id", "7"]).unwrap();
        assert_eq!(cli.cmd.client_id, 7);
        assert!(cli.cmd.rewards_token_mint.is_none());
        assert!(cli.cmd.subscription_epochs.is_empty());
    }

    #[test]
    fn parses_full_inspection_args() {
        let mint = Pubkey::new_unique();
        let cli = Cli::try_parse_from([
            "test",
            "--client-id",
            "7",
            "--rewards-token-mint",
            &mint.to_string(),
            "--subscription-epoch",
            "100",
            "--subscription-epoch",
            "101",
        ])
        .unwrap();
        assert_eq!(cli.cmd.rewards_token_mint, Some(mint));
        assert_eq!(cli.cmd.subscription_epochs, vec![100u64, 101]);
    }

    #[test]
    fn render_vcr_summary_uses_none_when_description_empty() {
        let info = ValidatorClientRewardsInfo {
            client_id: 7,
            manager_key: Pubkey::new_from_array([1u8; 32]),
            short_description: None,
            claim_holding_count: 0,
        };
        let key = Pubkey::new_from_array([2u8; 32]);
        let out = render_vcr_summary(7, &key, &info);
        assert!(out.contains("description         : (none)"));
        assert!(out.contains("claim holding count : 0"));
        assert!(out.contains(&info.manager_key.to_string()));
        assert!(out.contains(&key.to_string()));
    }

    #[test]
    fn render_vcr_summary_renders_description() {
        let info = ValidatorClientRewardsInfo {
            client_id: 7,
            manager_key: Pubkey::new_from_array([1u8; 32]),
            short_description: Some("acme".to_string()),
            claim_holding_count: 4,
        };
        let key = Pubkey::new_from_array([2u8; 32]);
        let out = render_vcr_summary(7, &key, &info);
        assert!(out.contains("description         : acme"));
        assert!(out.contains("claim holding count : 4"));
    }

    #[test]
    fn render_holding_row_distinguishes_statuses() {
        let key = Pubkey::new_from_array([3u8; 32]);
        let expected_mint = Pubkey::new_from_array([5u8; 32]);
        let other_mint = Pubkey::new_from_array([6u8; 32]);
        let wrong_owner = Pubkey::new_from_array([7u8; 32]);

        let balance = render_holding_row(100, &key, &TokenAccountStatus::Balance(1_234_567), None);
        assert!(balance.contains("epoch   100"));
        assert!(balance.contains("balance=1234567"));

        let missing = render_holding_row(101, &key, &TokenAccountStatus::DoesNotExist, None);
        assert!(missing.contains("(does not exist)"));

        let bad_owner = render_holding_row(
            102,
            &key,
            &TokenAccountStatus::WrongOwner(wrong_owner),
            None,
        );
        assert!(bad_owner.contains("(wrong owner:"));
        assert!(bad_owner.contains(&wrong_owner.to_string()));

        let malformed = render_holding_row(103, &key, &TokenAccountStatus::Malformed, None);
        assert!(malformed.contains("(malformed token account)"));

        let wrong_mint = render_holding_row(
            104,
            &key,
            &TokenAccountStatus::WrongMint(other_mint),
            Some(&expected_mint),
        );
        assert!(wrong_mint.contains("(wrong mint: found"));
        assert!(wrong_mint.contains(&other_mint.to_string()));
        assert!(wrong_mint.contains(&expected_mint.to_string()));
    }

    #[test]
    fn render_manager_ata_row_distinguishes_statuses() {
        let ata = Pubkey::new_from_array([4u8; 32]);
        let present = render_manager_ata_row(&ata, &TokenAccountStatus::Balance(9_876_543), None);
        let missing = render_manager_ata_row(&ata, &TokenAccountStatus::DoesNotExist, None);
        let wrong_owner_key = Pubkey::new_from_array([8u8; 32]);
        let wrong_owner =
            render_manager_ata_row(&ata, &TokenAccountStatus::WrongOwner(wrong_owner_key), None);

        assert!(present.contains("manager ATA"));
        assert!(present.contains(&ata.to_string()));
        assert!(present.contains("balance=9876543"));

        assert!(missing.contains("manager ATA"));
        assert!(missing.contains(&ata.to_string()));
        assert!(missing.contains("(does not exist)"));

        assert!(wrong_owner.contains("(wrong owner:"));
        assert!(wrong_owner.contains(&wrong_owner_key.to_string()));
    }
}
