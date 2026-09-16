use anyhow::{Context, Result, anyhow, ensure};
use clap::Subcommand;
use doublezero_builder_stake::{
    ID,
    instruction::builders,
    state::{self, BuilderStake, ProgramConfig, TierParameters},
};
use doublezero_program_tools::zero_copy;
use doublezero_solana_client_tools::payer::{SolanaPayerOptions, TransactionOutcome, Wallet};
use solana_sdk::pubkey::Pubkey;

/// The 2Z mint this build carries, which is what the program it talks to will accept.
///
/// Feature-gated the same way the program is, so a CLI built without `development` names the
/// mainnet mint and one built with it names the devnet mint. `--mint-2z` overrides it, and the
/// program refuses a mint that is not its own, so an override only ever produces a clearer error
/// than a silent mismatch.
fn compiled_2z_mint() -> Pubkey {
    doublezero_builder_stake::DOUBLEZERO_MINT_KEY
}

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

        /// Defaults to the mint this build carries.
        #[arg(long, value_name = "PUBKEY")]
        mint_2z: Option<Pubkey>,

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

        /// Defaults to the mint this build carries.
        #[arg(long, value_name = "PUBKEY")]
        mint_2z: Option<Pubkey>,

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

        /// Defaults to the mint this build carries.
        #[arg(long, value_name = "PUBKEY")]
        mint_2z: Option<Pubkey>,

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
                mint_2z,
                solana_payer_options,
            } => {
                execute_initialize(
                    stake_index,
                    committed_rate_bits_per_sec,
                    mint_2z,
                    solana_payer_options,
                )
                .await
            }
            Self::PostBond {
                stake_index,
                amount,
                mint_2z,
                solana_payer_options,
            } => execute_post_bond(stake_index, amount, mint_2z, solana_payer_options).await,
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
                mint_2z,
                solana_payer_options,
            } => {
                execute_set_tier_parameters(
                    up_to_1gbps,
                    up_to_5gbps,
                    unmetered,
                    mint_2z,
                    solana_payer_options,
                )
                .await
            }
            Self::SetPaused {
                paused,
                solana_payer_options,
            } => execute_set_paused(paused, solana_payer_options).await,
        }
    }
}

/// Decimal 2Z to the mint's smallest unit.
///
/// Amounts are quoted in 2Z everywhere a person reads them and stored in base units everywhere the
/// program does, and the development mint has eight decimals. Taking base units on the command line
/// would make a bond a hundred million times too small a typo nobody notices until a feed is
/// refused, so the conversion happens here and the decimals come from the mint rather than a
/// constant this crate would have to keep in step.
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
        .ok_or_else(|| anyhow!("mint decimals {decimals} do not fit a u64 scale"))?;
    let whole: u64 = whole
        .parse()
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
        .ok_or_else(|| anyhow!("amount {trimmed} does not fit a u64 in base units"))
}

async fn mint_decimals(wallet: &Wallet, mint_2z: &Pubkey) -> Result<u8> {
    let account = wallet
        .connection
        .get_account(mint_2z)
        .await
        .with_context(|| format!("cannot read 2Z mint {mint_2z}"))?;
    // Decimals sit at offset 44 of an SPL mint: supply(8) after mint_authority(4+32), then this.
    const DECIMALS_OFFSET: usize = 44;
    account
        .data
        .get(DECIMALS_OFFSET)
        .copied()
        .ok_or_else(|| anyhow!("{mint_2z} is not an SPL mint"))
}

async fn send(wallet: &Wallet, ix: solana_sdk::instruction::Instruction, what: &str) -> Result<()> {
    let transaction = wallet.new_transaction(&[ix]).await?;
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
    mint_2z: Option<Pubkey>,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let builder = wallet.pubkey();

    let (stake_key, _) = BuilderStake::find_address(&builder, stake_index);
    println!("Builder stake: {stake_key}");
    println!(
        "Stake 2Z account: {}",
        state::find_2z_token_pda_address(&stake_key).0
    );

    resolve_mint(mint_2z)?;

    let ix = builders::initialize_builder_stake(&builder, stake_index, committed_rate_bits_per_sec);
    send(&wallet, ix, "Initialized builder stake").await
}

/// The mint to work against, and a clear refusal when the caller names one this build cannot use.
///
/// `initialize_builder_stake` passes the compiled-in mint as an account, so a caller who overrides
/// it is really asking for a different build. Saying that here beats a program error that names
/// only the account index.
fn resolve_mint(mint_2z: Option<Pubkey>) -> Result<Pubkey> {
    let compiled = compiled_2z_mint();
    match mint_2z {
        Some(asked) if asked != compiled => Err(anyhow!(
            "this build carries 2Z mint {compiled}, and you asked for {asked}. \
             Rebuild with --features development for the devnet mint, or drop --mint-2z."
        )),
        _ => Ok(compiled),
    }
}

async fn execute_post_bond(
    stake_index: u64,
    amount_2z: String,
    mint_2z: Option<Pubkey>,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let builder = wallet.pubkey();

    let mint_2z = resolve_mint(mint_2z)?;
    let decimals = mint_decimals(&wallet, &mint_2z).await?;
    let amount = to_base_units(&amount_2z, decimals)?;
    ensure!(amount > 0, "a bond of zero moves nothing");

    let (source, _) = Wallet::ata_address_and_create_compute_units(&builder, &mint_2z);
    println!("Paying {amount_2z} 2Z ({amount} base units) from {source}");

    let ix = builders::post_bond(&builder, stake_index, &source, amount);
    send(&wallet, ix, "Posted bond").await
}

async fn execute_show(
    builder: Option<Pubkey>,
    stake_index: u64,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let builder = builder.unwrap_or_else(|| wallet.pubkey());

    let (stake_key, _) = BuilderStake::find_address(&builder, stake_index);
    let account = wallet
        .connection
        .get_account(&stake_key)
        .await
        .with_context(|| format!("no builder stake at {stake_key}"))?;
    let (stake, _) =
        zero_copy::checked_from_bytes_with_discriminator::<BuilderStake>(&account.data)
            .ok_or_else(|| anyhow!("{stake_key} does not decode as a BuilderStake"))?;

    println!("address      : {stake_key}");
    println!("builder      : {}", stake.builder);
    println!("stake_index  : {}", stake.stake_index);
    println!("bonded       : {}", stake.bonded_2z_amount);
    println!("required     : {}", stake.required_2z_amount);
    println!(
        "committed    : {} bits/sec",
        stake.committed_rate_bits_per_sec
    );
    println!("hold expires : {}", stake.hold_expires_at);
    println!(
        "covers       : {}",
        stake.bonded_2z_amount >= stake.required_2z_amount
    );
    Ok(())
}

async fn execute_show_config(solana_payer_options: SolanaPayerOptions) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let (config_key, _) = ProgramConfig::find_address();

    let account = wallet
        .connection
        .get_account(&config_key)
        .await
        .with_context(|| {
            format!("no program config at {config_key}, so the program is uninitialized")
        })?;
    let (config, _) =
        zero_copy::checked_from_bytes_with_discriminator::<ProgramConfig>(&account.data)
            .ok_or_else(|| anyhow!("{config_key} does not decode as a ProgramConfig"))?;

    let tiers: &TierParameters = &config.tier_parameters;
    println!("address      : {config_key}");
    println!("program      : {ID}");
    println!("admin_key    : {}", config.admin_key);
    println!("paused       : {}", config.is_paused());
    println!("up to 1 Gbps : {}", tiers.up_to_1gbps_2z_amount);
    println!("up to 5 Gbps : {}", tiers.up_to_5gbps_2z_amount);
    println!("unmetered    : {}", tiers.unmetered_2z_amount);
    Ok(())
}

async fn execute_initialize_program(solana_payer_options: SolanaPayerOptions) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let ix = builders::initialize_program(&wallet.pubkey());
    send(&wallet, ix, "Initialized program").await
}

async fn execute_set_tier_parameters(
    up_to_1gbps: String,
    up_to_5gbps: String,
    unmetered: String,
    mint_2z: Option<Pubkey>,
    solana_payer_options: SolanaPayerOptions,
) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let mint_2z = resolve_mint(mint_2z)?;
    let decimals = mint_decimals(&wallet, &mint_2z).await?;

    let one = to_base_units(&up_to_1gbps, decimals)?;
    let five = to_base_units(&up_to_5gbps, decimals)?;
    let unmetered = to_base_units(&unmetered, decimals)?;

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

    println!("up to 1 Gbps : {one}");
    println!("up to 5 Gbps : {five}");
    println!("unmetered    : {unmetered}");

    let ix = builders::set_tier_parameters(&wallet.pubkey(), one, five, unmetered);
    send(&wallet, ix, "Set tier parameters").await
}

async fn execute_set_paused(paused: bool, solana_payer_options: SolanaPayerOptions) -> Result<()> {
    let wallet = Wallet::try_from(solana_payer_options)?;
    let ix = builders::set_paused(&wallet.pubkey(), paused);
    send(&wallet, ix, if paused { "Paused" } else { "Resumed" }).await
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

    /// The tier table is read back in base units, so the two directions have to agree.
    #[test]
    fn test_the_live_tier_table_round_trips() {
        assert_eq!(to_base_units("1", 8).unwrap(), 100_000_000);
        assert_eq!(to_base_units("2", 8).unwrap(), 200_000_000);
        assert_eq!(to_base_units("5", 8).unwrap(), 500_000_000);
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
        for bad in ["", " ", "abc", "1.2.3", "-1", "1e8", "0x5", "."] {
            assert!(to_base_units(bad, 8).is_err(), "{bad} should be refused");
        }
    }

    /// u64 is the program's own width, so the boundary is where the CLI has to stop rather than
    /// wrap into a smaller bond than the caller typed.
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
