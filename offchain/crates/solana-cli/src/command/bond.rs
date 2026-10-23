//! A builder's bond on the `builder-bond` program: create it, post 2Z to it, read it back, and
//! withdraw what it holds above its tier's requirement once the hold has elapsed.
//!
//! These are the builder's own transactions, signed with its own key. The program's admin
//! commands (the tier table, the admin key, pausing) are not here: a builder never runs them.

use std::io::Write;

use anyhow::{Context, Result, anyhow, bail};
use chrono::{DateTime, Utc};
use clap::{Args, Subcommand};
use doublezero_cli_core::{CliContext, validators::validate_parse_bandwidth};
use doublezero_solana_client_tools::{
    keypair::try_load_keypair,
    payer::TransactionOutcome,
    rpc::{SolanaConnection, SolanaConnectionOptions},
};
use doublezero_solana_sdk::{
    DOUBLEZERO_MINT_DECIMALS,
    builder_bond::{instruction::builders, state::BuilderBond},
    environment_2z_token_mint_key,
};
use solana_sdk::{instruction::Instruction, pubkey::Pubkey};
use spl_associated_token_account_interface::address::get_associated_token_address;

use crate::command::{WriteVerbOptions, build_wallet, resolve_network_env, solana_connection};

#[derive(Debug, Args)]
pub struct BondCommand {
    #[command(subcommand)]
    pub command: BondSubcommand,
}

impl BondCommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        self.command.execute(ctx, out).await
    }
}

#[derive(Debug, Subcommand)]
pub enum BondSubcommand {
    /// Create a bond and its 2Z account. Posting needs both to exist first.
    Init(InitCommand),
    /// Move 2Z from your own 2Z account into the bond. The first post starts the hold.
    Post(PostCommand),
    /// Print a bond. Reads only, signs nothing.
    Show(ShowCommand),
    /// Take back 2Z the bond holds above what its tier requires, once the hold has elapsed.
    Withdraw(WithdrawCommand),
}

impl BondSubcommand {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Self::Init(command) => command.execute(ctx, out).await,
            Self::Post(command) => command.execute(ctx, out).await,
            Self::Show(command) => command.execute(ctx, out).await,
            Self::Withdraw(command) => command.execute(ctx, out).await,
        }
    }
}

#[derive(Debug, Args)]
pub struct InitCommand {
    /// The rate the feed backed by this bond may commit to (e.g. 1Gbps). It sets the tier, and
    /// the tier sets what the bond must hold.
    #[arg(long, value_parser = validate_parse_bandwidth)]
    committed_rate: u64,
    /// Which of your bonds. Part of its address, so it cannot change.
    #[arg(long, default_value_t = 0)]
    bond_index: u64,
    #[command(flatten)]
    write_opts: WriteVerbOptions,
}

impl InitCommand {
    async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let moniker_env = explicit_moniker_env(&self.write_opts.connection_options);
        let wallet = build_wallet(ctx, self.write_opts)?;
        let builder = wallet.pubkey();
        // The program checks this mint, and the crate's own constant is fixed at build time,
        // so a client that serves several networks names it.
        let mint = environment_2z_token_mint_key(
            resolve_network_env(&wallet.connection, moniker_env).await?,
        );
        let ix = builders::initialize_builder_bond_with_mint(
            &builder,
            self.bond_index,
            self.committed_rate,
            &mint,
        );
        writeln!(
            out,
            "Bond:      {}",
            bond_address(&builder, self.bond_index)
        )?;
        send(&wallet, &[ix], "Created the bond", out).await
    }
}

#[derive(Debug, Args)]
pub struct PostCommand {
    /// Amount in 2Z, as a decimal. `1` is one 2Z, not one base unit.
    #[arg(long)]
    amount: String,
    #[arg(long, default_value_t = 0)]
    bond_index: u64,
    /// The 2Z account to pay from. Defaults to your associated 2Z account.
    #[arg(long)]
    source: Option<Pubkey>,
    #[command(flatten)]
    write_opts: WriteVerbOptions,
}

impl PostCommand {
    async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let amount = to_base_units(&self.amount)?;
        let moniker_env = explicit_moniker_env(&self.write_opts.connection_options);
        let wallet = build_wallet(ctx, self.write_opts)?;
        let builder = wallet.pubkey();
        let source = match self.source {
            Some(source) => source,
            None => own_2z_account(&wallet.connection, moniker_env, &builder).await?,
        };
        let ix = builders::post_bond(&builder, self.bond_index, &source, amount);
        writeln!(
            out,
            "Bond:      {}",
            bond_address(&builder, self.bond_index)
        )?;
        writeln!(out, "Amount:    {}", in_2z(amount))?;
        send(&wallet, &[ix], "Posted to the bond", out).await
    }
}

#[derive(Debug, Args)]
pub struct WithdrawCommand {
    /// Amount in 2Z, as a decimal.
    #[arg(long)]
    amount: String,
    #[arg(long, default_value_t = 0)]
    bond_index: u64,
    /// Where the 2Z goes. Defaults to your associated 2Z account.
    #[arg(long)]
    destination: Option<Pubkey>,
    #[command(flatten)]
    write_opts: WriteVerbOptions,
}

impl WithdrawCommand {
    async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let amount = to_base_units(&self.amount)?;
        let moniker_env = explicit_moniker_env(&self.write_opts.connection_options);
        let wallet = build_wallet(ctx, self.write_opts)?;
        let builder = wallet.pubkey();
        let destination = match self.destination {
            Some(destination) => destination,
            None => own_2z_account(&wallet.connection, moniker_env, &builder).await?,
        };
        let ix = builders::withdraw(&builder, self.bond_index, &destination, amount);
        writeln!(
            out,
            "Bond:      {}",
            bond_address(&builder, self.bond_index)
        )?;
        writeln!(out, "Amount:    {}", in_2z(amount))?;
        send(&wallet, &[ix], "Withdrew from the bond", out).await
    }
}

#[derive(Debug, Args)]
pub struct ShowCommand {
    /// Whose bond. Defaults to your own key.
    #[arg(long)]
    builder: Option<Pubkey>,
    #[arg(long, default_value_t = 0)]
    bond_index: u64,
    /// Print the bond as JSON.
    #[arg(long)]
    json: bool,
    #[command(flatten)]
    connection_options: SolanaConnectionOptions,
}

impl ShowCommand {
    async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        let builder = match self.builder {
            Some(builder) => builder,
            None => own_key(ctx)?,
        };
        let connection = solana_connection(ctx, &self.connection_options);
        let key = bond_address(&builder, self.bond_index);
        let bond = connection
            .try_fetch_zero_copy_data::<BuilderBond>(&key)
            .await
            .with_context(|| format!("reading bond {} for {builder} at {key}", self.bond_index))?;
        let view = BondView::new(&key, &bond, Utc::now().timestamp());
        if self.json {
            writeln!(out, "{}", serde_json::to_string_pretty(&view)?)?;
        } else {
            write!(out, "{}", view.text())?;
        }
        Ok(())
    }
}

/// What `show` prints. Split out so a test can check it without a cluster. Amounts are 2Z base
/// units; `committed_rate_bits_per_sec` is `u64::MAX` for an unmetered bond, and
/// `hold_expires_at` is `None` until the first post starts the hold.
#[derive(Debug, serde::Serialize)]
struct BondView {
    address: String,
    builder: String,
    bond_index: u64,
    committed_rate_bits_per_sec: u64,
    bonded_2z_amount: u64,
    required_2z_amount: u64,
    covers: bool,
    hold_expires_at: Option<i64>,
    withdrawable_2z_amount: u64,
}

impl BondView {
    fn new(key: &Pubkey, bond: &BuilderBond, now: i64) -> Self {
        Self {
            address: key.to_string(),
            builder: bond.builder.to_string(),
            bond_index: bond.bond_index,
            committed_rate_bits_per_sec: bond.committed_rate_bits_per_sec,
            bonded_2z_amount: bond.bonded_2z_amount,
            required_2z_amount: bond.required_2z_amount,
            covers: bond.is_funded(),
            hold_expires_at: bond.hold_started().then_some(bond.hold_expires_at),
            withdrawable_2z_amount: bond.withdrawable_2z_amount(now),
        }
    }

    fn text(&self) -> String {
        let hold = match self.hold_expires_at {
            Some(at) => DateTime::<Utc>::from_timestamp(at, 0)
                .map_or_else(|| at.to_string(), |at| at.to_rfc3339()),
            None => "not started: nothing posted yet".to_string(),
        };
        let rate = if self.committed_rate_bits_per_sec == u64::MAX {
            "unmetered".to_string()
        } else {
            format!("{} bits/sec", self.committed_rate_bits_per_sec)
        };
        format!(
            "Address:      {}\n\
             Builder:      {}\n\
             Bond index:   {}\n\
             Committed:    {rate}\n\
             Bonded:       {}\n\
             Required:     {}\n\
             Covers:       {}\n\
             Hold expires: {hold}\n\
             Withdrawable: {}\n",
            self.address,
            self.builder,
            self.bond_index,
            in_2z(self.bonded_2z_amount),
            in_2z(self.required_2z_amount),
            self.covers,
            in_2z(self.withdrawable_2z_amount),
        )
    }
}

/// The network a per-verb `-u` names, or `None` so the connection's genesis hash decides.
/// `moniker_env` alone reports mainnet whenever `-u` is absent, which picks the mainnet mint for
/// a run that reaches devnet through the global `--solana-url`.
fn explicit_moniker_env(
    options: &SolanaConnectionOptions,
) -> Option<doublezero_solana_sdk::NetworkEnvironment> {
    options
        .solana_url_or_moniker
        .as_ref()
        .and_then(|_| options.moniker_env())
}

fn bond_address(builder: &Pubkey, bond_index: u64) -> Pubkey {
    BuilderBond::find_address(builder, bond_index).0
}

/// The signer's associated 2Z account, for the network the connection reaches.
async fn own_2z_account(
    connection: &SolanaConnection,
    moniker_env: Option<doublezero_solana_sdk::NetworkEnvironment>,
    owner: &Pubkey,
) -> Result<Pubkey> {
    let mint = environment_2z_token_mint_key(resolve_network_env(connection, moniker_env).await?);
    Ok(get_associated_token_address(owner, &mint))
}

/// The key `show` reads for when no `--builder` is given. Loaded the way the write verbs load
/// theirs, so `show` finds the bond `init` created with the same options.
fn own_key(ctx: &CliContext) -> Result<Pubkey> {
    let keypair = try_load_keypair(ctx.keypair_path.clone())
        .context("loading your keypair: pass --keypair, or --builder to read another bond")?;
    Ok(solana_sdk::signer::Signer::pubkey(&keypair))
}

async fn send(
    wallet: &doublezero_solana_client_tools::payer::Wallet,
    instructions: &[Instruction],
    done: &str,
    out: &mut impl Write,
) -> Result<()> {
    // `Wallet` holds the price instruction but does not add it, so a verb that skips this
    // accepts `--with-compute-unit-price` and drops it.
    let instructions: Vec<Instruction> = wallet
        .compute_unit_price_ix
        .iter()
        .cloned()
        .chain(instructions.iter().cloned())
        .collect();
    let transaction = wallet.new_transaction(&instructions).await?;
    if let TransactionOutcome::Executed(signature) =
        wallet.send_or_simulate_transaction(&transaction).await?
    {
        writeln!(out, "{done}: {signature}")?;
        wallet.write_verbose_output(out, &[signature]).await?;
    }
    Ok(())
}

/// A decimal number of 2Z in the mint's smallest unit. Exact: a money amount never passes
/// through a float.
fn to_base_units(amount_2z: &str) -> Result<u64> {
    let trimmed = amount_2z.trim();
    let (whole, fraction) = trimmed.split_once('.').unwrap_or((trimmed, ""));
    if whole.is_empty() || !whole.chars().all(|c| c.is_ascii_digit()) {
        bail!("{trimmed} is not a decimal number of 2Z");
    }
    if !fraction.chars().all(|c| c.is_ascii_digit()) {
        bail!("{trimmed} is not a decimal number of 2Z");
    }
    let decimals = usize::from(DOUBLEZERO_MINT_DECIMALS);
    if fraction.len() > decimals {
        bail!("{trimmed} has more than {decimals} decimal places, which 2Z cannot hold");
    }
    let scale = 10u64.pow(u32::from(DOUBLEZERO_MINT_DECIMALS));
    let fraction: u64 = format!("{fraction:0<decimals$}").parse()?;
    whole
        .parse::<u64>()
        .ok()
        .and_then(|whole| whole.checked_mul(scale))
        .and_then(|scaled| scaled.checked_add(fraction))
        .ok_or_else(|| anyhow!("{trimmed} 2Z does not fit a u64 in base units"))
}

/// Base units as 2Z, with the base units alongside so either can be checked.
fn in_2z(base_units: u64) -> String {
    let scale = 10u64.pow(u32::from(DOUBLEZERO_MINT_DECIMALS));
    let decimals = usize::from(DOUBLEZERO_MINT_DECIMALS);
    format!(
        "{}.{:0decimals$} 2Z ({base_units} base units)",
        base_units / scale,
        base_units % scale
    )
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn to_base_units_is_exact() {
        assert_eq!(to_base_units("1").unwrap(), 100_000_000);
        assert_eq!(to_base_units("1.5").unwrap(), 150_000_000);
        assert_eq!(to_base_units("0.00000001").unwrap(), 1);
        assert_eq!(to_base_units(" 2.0 ").unwrap(), 200_000_000);
        for bad in ["", "-1", "1.2.3", "1e3", ".5", "1.000000001", "abc"] {
            assert!(to_base_units(bad).is_err(), "{bad:?}");
        }
        assert!(to_base_units("184467440737.09551616").is_err(), "past u64");
    }

    #[test]
    fn in_2z_shows_both_units() {
        assert_eq!(in_2z(150_000_001), "1.50000001 2Z (150000001 base units)");
        assert_eq!(in_2z(0), "0.00000000 2Z (0 base units)");
    }

    #[test]
    fn show_reports_the_hold_and_what_can_be_withdrawn() {
        let builder = Pubkey::new_unique();
        let key = bond_address(&builder, 0);
        // Field by field: the struct's padding is private to the program crate.
        let mut bond = BuilderBond::default();
        bond.builder = builder;
        bond.committed_rate_bits_per_sec = 1_000_000_000;
        bond.required_2z_amount = 100_000_000;

        let unposted = BondView::new(&key, &bond, 0).text();
        assert!(unposted.contains("Hold expires: not started"), "{unposted}");
        assert!(unposted.contains("Covers:       false"), "{unposted}");

        bond.bonded_2z_amount = 150_000_000;
        bond.hold_expires_at = 1_800_000_000;
        let held = BondView::new(&key, &bond, 1_799_999_999).text();
        assert!(
            held.contains("Hold expires: 2027-01-15T08:00:00+00:00"),
            "{held}"
        );
        assert!(held.contains("Withdrawable: 0.00000000 2Z"), "{held}");
        assert!(held.contains("Committed:    1000000000 bits/sec"), "{held}");

        let elapsed = BondView::new(&key, &bond, 1_800_000_000).text();
        assert!(elapsed.contains("Withdrawable: 0.50000000 2Z"), "{elapsed}");

        bond.committed_rate_bits_per_sec = u64::MAX;
        assert!(
            BondView::new(&key, &bond, 0)
                .text()
                .contains("Committed:    unmetered")
        );
    }

    #[test]
    fn only_a_per_verb_moniker_overrides_the_genesis_hash() {
        let unset = SolanaConnectionOptions {
            solana_url_or_moniker: None,
        };
        assert_eq!(explicit_moniker_env(&unset), None);
        let devnet = SolanaConnectionOptions {
            solana_url_or_moniker: Some("devnet".to_string()),
        };
        assert_eq!(
            explicit_moniker_env(&devnet),
            Some(doublezero_solana_sdk::NetworkEnvironment::Devnet)
        );
    }

    #[test]
    fn show_json_has_base_units_and_no_hold_before_the_first_post() {
        let builder = Pubkey::new_unique();
        let key = bond_address(&builder, 0);
        let mut bond = BuilderBond::default();
        bond.builder = builder;
        bond.required_2z_amount = 100_000_000;

        let unposted = serde_json::to_value(BondView::new(&key, &bond, 0)).unwrap();
        assert_eq!(unposted["address"], key.to_string());
        assert_eq!(unposted["hold_expires_at"], serde_json::Value::Null);
        assert_eq!(unposted["required_2z_amount"], 100_000_000);

        bond.bonded_2z_amount = 150_000_000;
        bond.hold_expires_at = 1_800_000_000;
        let elapsed = serde_json::to_value(BondView::new(&key, &bond, 1_800_000_000)).unwrap();
        assert_eq!(elapsed["hold_expires_at"], 1_800_000_000);
        assert_eq!(elapsed["withdrawable_2z_amount"], 50_000_000);
        assert_eq!(elapsed["covers"], true);
    }
}
