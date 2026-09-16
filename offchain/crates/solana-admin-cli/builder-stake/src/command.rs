use anyhow::{Context, Result, ensure};
use clap::Subcommand;
use doublezero_builder_stake::{
    DOUBLEZERO_MINT_DECIMALS, DOUBLEZERO_MINT_KEY, ID,
    instruction::builders,
    state::{self, BuilderStake, ProgramConfig},
};
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, Subcommand)]
pub enum BuilderStakeAdminSubcommand {
    /// Create a BuilderStake and its 2Z token account, which PostBond needs to exist first.
    Initialize {
        /// Which of this builder's stakes. Part of the address, so it cannot be changed later.
        #[arg(long, default_value_t = 0)]
        stake_index: u64,

        /// The rate the feed backed by this stake may commit to, in bits per second.
        #[arg(long, value_name = "BITS_PER_SEC")]
        committed_rate_bits_per_sec: u64,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Move 2Z from the payer's token account into a stake.
    PostBond {
        #[arg(long, default_value_t = 0)]
        stake_index: u64,

        /// Amount in 2Z, decimal. Converted using the mint's own decimals, so `1` is one 2Z.
        #[arg(long, value_name = "2Z")]
        amount: String,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Take back 2Z the stake holds above what its tier requires, once the hold has elapsed.
    Withdraw {
        #[arg(long, default_value_t = 0)]
        stake_index: u64,

        /// Amount in 2Z, decimal.
        #[arg(long, value_name = "2Z")]
        amount: String,

        /// Where the 2Z goes. Defaults to the payer's own 2Z account.
        #[arg(long, value_name = "PUBKEY")]
        destination_token_account: Option<Pubkey>,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Read a BuilderStake back.
    Show {
        /// Defaults to the payer.
        #[arg(long, value_name = "PUBKEY")]
        builder: Option<Pubkey>,

        #[arg(long, default_value_t = 0)]
        stake_index: u64,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Read ProgramConfig back: the admin, the pause flag and the tier table.
    ShowConfig {
        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Initialize ProgramConfig, setting the admin to the upgrade authority.
    ///
    /// A fresh deployment starts paused with a zeroed tier table and can size no bond, so this
    /// and SetTierParameters are both needed before any builder can post one.
    InitializeProgram {
        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Set what a bond costs at each tier, in 2Z.
    ///
    /// The program refuses a table that is not well formed: the 1Gbps amount must be non-zero and
    /// the three must not decrease, because a cheaper higher tier would mean every builder buys
    /// the cheap one and commits to the higher rate.
    SetTierParameters {
        #[arg(long, value_name = "2Z")]
        up_to_1gbps: String,

        #[arg(long, value_name = "2Z")]
        up_to_5gbps: String,

        #[arg(long, value_name = "2Z")]
        unmetered: String,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Hand the admin to another key. The payer has to be the program's upgrade authority.
    SetAdmin {
        #[arg(long, value_name = "PUBKEY")]
        admin_key: Pubkey,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },

    /// Pause or resume the program. A paused program takes no bonds.
    SetPaused {
        #[arg(long, action = clap::ArgAction::Set)]
        paused: bool,

        #[command(flatten)]
        solana_payer_options: SolanaPayerOptions,
    },
}

impl BuilderStakeAdminSubcommand {
    pub async fn try_into_execute(self) -> Result<()> {
        match self {
            Self::Initialize {
                stake_index,
                committed_rate_bits_per_sec,
                solana_payer_options,
            } => {
                execute_initialize(
                    stake_index,
                    committed_rate_bits_per_sec,
                    solana_payer_options,
                )
                .await
            }
            Self::PostBond {
                stake_index,
                amount,
                solana_payer_options,
            } => execute_post_bond(stake_index, amount, solana_payer_options).await,
            Self::Withdraw {
                stake_index,
                amount,
                destination_token_account,
                solana_payer_options,
            } => {
                execute_withdraw(
                    stake_index,
                    amount,
                    destination_token_account,
                    solana_payer_options,
                )
                .await
            }
            Self::Show {
                builder,
                stake_index,
                solana_payer_options,
            } => execute_show(builder, stake_index, solana_payer_options).await,
            Self::ShowConfig {
                solana_payer_options,
            } => execute_show_config(solana_payer_options).await,
            Self::InitializeProgram {
                solana_payer_options,
            } => execute_initialize_program(solana_payer_options).await,
            Self::SetTierParameters {
                up_to_1gbps,
                up_to_5gbps,
                unmetered,
                solana_payer_options,
            } => {
                execute_set_tier_parameters(
                    up_to_1gbps,
                    up_to_5gbps,
                    unmetered,
                    solana_payer_options,
                )
                .await
            }
            Self::SetAdmin {
                admin_key,
                solana_payer_options,
            } => execute_set_admin(admin_key, solana_payer_options).await,
            Self::SetPaused {
                paused,
                solana_payer_options,
            } => execute_set_paused(paused, solana_payer_options).await,
        }
    }
}

/// Base units back to 2Z for display, so a reader is not dividing by a hundred million to check
/// a number they typed in 2Z.
fn in_2z(base_units: u64) -> String {
    let scale = 10u64.pow(DOUBLEZERO_MINT_DECIMALS as u32);
    format!(
        "{}.{:0width$} 2Z ({base_units} base units)",
        base_units / scale,
        base_units % scale,
        width = DOUBLEZERO_MINT_DECIMALS as usize
    )
}

/// Decimal 2Z to the mint's smallest unit.
///
/// Amounts are quoted in 2Z everywhere a person reads them and stored in base units everywhere the
/// program does. Taking base units on the command line would make a bond a hundred million times
/// too small a typo nobody notices until a feed is refused, so the conversion happens here.
fn to_base_units(amount_2z: &str, decimals: u8) -> Result<u64> {
    let trimmed = amount_2z.trim();
    ensure!(!trimmed.is_empty(), "amount is empty");

    let (whole, fraction) = match trimmed.split_once('.') {
        Some((whole, fraction)) => (whole, fraction),
        None => (trimmed, ""),
    };
    ensure!(
        whole.chars().all(|c| c.is_ascii_digit()) && !whole.is_empty(),
        "amount {trimmed} is not a decimal number"
    );
    ensure!(
        fraction.chars().all(|c| c.is_ascii_digit()),
        "amount {trimmed} is not a decimal number"
    );
    ensure!(
        fraction.len() <= decimals as usize,
        "amount {trimmed} has more than {decimals} decimal places, which this mint cannot hold"
    );

    let scale = 10u64
        .checked_pow(decimals as u32)
        .with_context(|| format!("mint decimals {decimals} do not fit a u64 scale"))?;
    let whole = whole
        .parse::<u64>()
        .with_context(|| format!("whole part of {trimmed} does not fit a u64"))?;
    let padded = format!("{fraction:0<width$}", width = decimals as usize);
    let fraction: u64 = if padded.is_empty() {
        0
    } else {
        padded.parse()?
    };

    whole
        .checked_mul(scale)
        .and_then(|scaled| scaled.checked_add(fraction))
        .with_context(|| format!("amount {trimmed} does not fit a u64 in base units"))
}

async fn send(
    wallet: &Wallet,
    ixs: Vec<solana_sdk::instruction::Instruction>,
    what: &str,
) -> Result<()> {
    let mut ixs = ixs;
    if let Some(compute_unit_price_ix) = wallet.compute_unit_price_ix.clone() {
        ixs.push(compute_unit_price_ix);
    }
    let transaction = wallet.new_transaction(&ixs).await?;
    if let TransactionOutcome::Executed(tx_sig) =
        wallet.send_or_simulate_transaction(&transaction).await?
    {
        println!("{what}: {tx_sig}");
        wallet.print_verbose_output(&[tx_sig]).await?;
    }
    Ok(())
}

async fn execute_initialize(
    stake_index: u64,
    committed_rate_bits_per_sec: u64,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let builder_key = wallet.pubkey();

    let (stake_key, _) = BuilderStake::find_address(&builder_key, stake_index);
    println!("Builder stake: {stake_key}");
    println!(
        "Stake 2Z account: {}",
        state::find_2z_token_pda_address(&stake_key).0
    );

    let ix =
        builders::initialize_builder_stake(&builder_key, stake_index, committed_rate_bits_per_sec);
    send(&wallet, vec![ix], "Initialized builder stake").await
}

async fn execute_post_bond(
    stake_index: u64,
    amount_2z: String,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let builder_key = wallet.pubkey();

    let amount = to_base_units(&amount_2z, DOUBLEZERO_MINT_DECIMALS)?;
    ensure!(amount > 0, "a bond of zero moves nothing");

    let (source_ata_key, _) =
        Wallet::ata_address_and_create_compute_units(&builder_key, &DOUBLEZERO_MINT_KEY);

    // Without this the token program refuses a short balance with an error naming neither the
    // account nor the amount, which is the failure this crate exists to stop an operator hitting.
    let held = wallet
        .connection
        .get_token_account_balance(&source_ata_key)
        .await
        .with_context(|| format!("cannot read 2Z account {source_ata_key}"))?;
    let held_base_units = held.amount.parse::<u64>().unwrap_or_default();
    ensure!(
        held_base_units >= amount,
        "{source_ata_key} holds {} 2Z and the bond is {amount_2z} 2Z",
        held.ui_amount_string
    );

    println!("Paying {amount_2z} 2Z ({amount} base units) from {source_ata_key}");

    let ix = builders::post_bond(&builder_key, stake_index, &source_ata_key, amount);
    send(&wallet, vec![ix], "Posted bond").await
}

async fn execute_withdraw(
    stake_index: u64,
    amount_2z: String,
    destination_token_account: Option<Pubkey>,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let builder_key = wallet.pubkey();
    let amount = to_base_units(&amount_2z, DOUBLEZERO_MINT_DECIMALS)?;

    // The program computes what is withdrawable from the hold and the tier, and refuses more. Say
    // which of the two reasons applies before sending, because its one error cannot.
    let (stake_key, _) = BuilderStake::find_address(&builder_key, stake_index);
    let stake = wallet
        .connection
        .try_fetch_zero_copy_data::<BuilderStake>(&stake_key)
        .await
        .with_context(|| format!("cannot read builder stake {stake_key}"))?;
    ensure!(
        stake.hold_started(),
        "no bond has been posted to {stake_key}"
    );
    let now = wallet
        .connection
        .get_block_time(wallet.connection.get_slot().await?)
        .await?;
    let withdrawable = stake.withdrawable_2z_amount(now);
    ensure!(
        withdrawable > 0,
        "nothing is withdrawable yet: the hold on {stake_key} runs to {}",
        stake.hold_expires_at
    );
    ensure!(
        amount <= withdrawable,
        "{amount_2z} 2Z is more than the {withdrawable} base units above this stake's requirement"
    );

    let destination_token_account_key = destination_token_account.unwrap_or_else(|| {
        Wallet::ata_address_and_create_compute_units(&builder_key, &DOUBLEZERO_MINT_KEY).0
    });
    println!("Sending {amount} base units to {destination_token_account_key}");

    let ix = builders::withdraw(
        &builder_key,
        stake_index,
        &destination_token_account_key,
        amount,
    );
    send(&wallet, vec![ix], "Withdrew").await
}

async fn execute_show(
    builder: Option<Pubkey>,
    stake_index: u64,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let builder_key = builder.unwrap_or_else(|| wallet.pubkey());

    let (stake_key, _) = BuilderStake::find_address(&builder_key, stake_index);
    let stake = wallet
        .connection
        .try_fetch_zero_copy_data::<BuilderStake>(&stake_key)
        .await
        .with_context(|| format!("cannot read builder stake {stake_key}"))?;

    println!("address      : {stake_key}");
    println!("builder      : {}", stake.builder);
    println!("stake_index  : {}", stake.stake_index);
    println!("bonded       : {}", in_2z(stake.bonded_2z_amount));
    println!("required     : {}", in_2z(stake.required_2z_amount));
    println!(
        "committed    : {} bits/sec",
        stake.committed_rate_bits_per_sec
    );
    if stake.hold_started() {
        println!("hold expires : {}", stake.hold_expires_at);
    } else {
        println!("hold expires : not started, no bond posted");
    }
    println!("covers       : {}", stake.is_funded());
    Ok(())
}

async fn execute_show_config(solana_payer_options: SolanaPayerOptions) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let (config_key, _) = ProgramConfig::find_address();

    let config = wallet
        .connection
        .try_fetch_zero_copy_data::<ProgramConfig>(&config_key)
        .await
        .with_context(|| format!("cannot read program config {config_key}"))?;

    let tiers = &config.tier_parameters;
    println!("address      : {config_key}");
    println!("program      : {ID}");
    println!("admin_key    : {}", config.admin_key);
    println!("paused       : {}", config.is_paused());
    println!("up to 1 Gbps : {}", in_2z(tiers.up_to_1gbps_2z_amount));
    println!("up to 5 Gbps : {}", in_2z(tiers.up_to_5gbps_2z_amount));
    println!("unmetered    : {}", in_2z(tiers.unmetered_2z_amount));
    Ok(())
}

async fn execute_initialize_program(solana_payer_options: SolanaPayerOptions) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let upgrade_authority_key = wallet.pubkey();
    send(
        &wallet,
        vec![
            builders::initialize_program(&upgrade_authority_key),
            builders::set_admin(&upgrade_authority_key, &upgrade_authority_key),
        ],
        "Initialized program and set admin",
    )
    .await
}

async fn execute_set_admin(
    admin_key: Pubkey,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let ix = builders::set_admin(&wallet.pubkey(), &admin_key);
    send(&wallet, vec![ix], "Set admin").await
}

async fn execute_set_tier_parameters(
    up_to_1gbps: String,
    up_to_5gbps: String,
    unmetered: String,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let one = to_base_units(&up_to_1gbps, DOUBLEZERO_MINT_DECIMALS)?;
    let five = to_base_units(&up_to_5gbps, DOUBLEZERO_MINT_DECIMALS)?;
    let unmetered = to_base_units(&unmetered, DOUBLEZERO_MINT_DECIMALS)?;

    // The program refuses this too. Saying it here costs a round trip less and names which pair is
    // wrong, which the program's one error code cannot.
    ensure!(
        one > 0,
        "the 1 Gbps tier cannot be zero, or a feed deploys against nothing"
    );
    ensure!(
        one <= five,
        "1 Gbps ({one}) costs more than 5 Gbps ({five})"
    );
    ensure!(
        five <= unmetered,
        "5 Gbps ({five}) costs more than unmetered ({unmetered})"
    );

    println!("up to 1 Gbps : {}", in_2z(one));
    println!("up to 5 Gbps : {}", in_2z(five));
    println!("unmetered    : {}", in_2z(unmetered));

    let ix = builders::set_tier_parameters(&wallet.pubkey(), one, five, unmetered);
    send(&wallet, vec![ix], "Set tier parameters").await
}

async fn execute_set_paused(paused: bool, solana_payer_options: SolanaPayerOptions) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let ix = builders::set_paused(&wallet.pubkey(), paused);
    send(&wallet, vec![ix], if paused { "Paused" } else { "Resumed" }).await
}

#[cfg(test)]
mod tests {
    use super::to_base_units;

    /// The whole point of taking 2Z rather than base units: `1` is one 2Z, not one hundred
    /// millionth of one. A bond off by this factor is refused by the coverage check with no hint
    /// that the amount was the problem.
    #[test]
    fn test_decimal_2z_becomes_base_units() {
        assert_eq!(to_base_units("1", 8).unwrap(), 100_000_000);
        assert_eq!(to_base_units("5", 8).unwrap(), 500_000_000);
        assert_eq!(to_base_units("0.5", 8).unwrap(), 50_000_000);
        assert_eq!(to_base_units("1.00000001", 8).unwrap(), 100_000_001);
        assert_eq!(to_base_units("0", 8).unwrap(), 0);
    }

    /// Decimals come from the mint, so a mint with fewer of them has to refuse the extra digits
    /// rather than silently truncate a bond downward.
    #[test]
    fn test_more_precision_than_the_mint_holds_is_refused() {
        assert!(to_base_units("1.123456789", 8).is_err());
        assert!(to_base_units("0.01", 1).is_err());
        assert_eq!(to_base_units("0.1", 1).unwrap(), 1);
    }

    #[test]
    fn test_non_numbers_are_refused() {
        for bad in [
            "", " ", "abc", "1.2.3", "-1", "+1", "1.+1", "1e8", "0x5", ".",
        ] {
            assert!(to_base_units(bad, 8).is_err(), "{bad} should be refused");
        }
    }

    /// u64 is the program's own width, so the boundary is where the CLI has to stop rather than
    /// wrap into a smaller bond than the caller typed.
    /// The fraction is added after the whole part is scaled, so the sum is where the last base
    /// units cross u64, not the multiply. Without this the checked_add can be a plain + and every
    /// other test still passes, while a bond one base unit over wraps to nearly nothing.
    #[test]
    fn test_the_fraction_cannot_push_the_sum_past_u64() {
        // u64::MAX is exactly 184467440737.09551615 2Z at eight decimals.
        assert_eq!(to_base_units("184467440737.09551615", 8).unwrap(), u64::MAX);
        assert!(to_base_units("184467440737.09551616", 8).is_err());
    }

    /// Decimals are whatever byte the mint holds, not a value this crate chose, so the scale has
    /// to hold at both ends: zero decimals must still accept a whole number, and a byte too large
    /// to raise ten to must report rather than panic.
    #[test]
    fn test_mint_decimals_at_both_ends() {
        assert_eq!(to_base_units("5", 0).unwrap(), 5);
        assert!(to_base_units("5.1", 0).is_err());
        assert_eq!(to_base_units("1", 19).unwrap(), 10_000_000_000_000_000_000);
        assert!(to_base_units("1", 20).is_err());
    }

    #[test]
    fn test_amounts_past_u64_are_refused() {
        // 184467440737 scales to 18446744073700000000, which still fits, so the boundary sits
        // between these two rather than at a round number.
        assert_eq!(
            to_base_units("184467440737", 8).unwrap(),
            18_446_744_073_700_000_000
        );
        assert!(to_base_units("184467440738", 8).is_err());
        assert!(to_base_units("18446744073709551616", 8).is_err());
    }
}
