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
        let moniker_env = self.write_opts.connection_options.moniker_env();
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
        let moniker_env = self.write_opts.connection_options.moniker_env();
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
        let moniker_env = self.write_opts.connection_options.moniker_env();
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
            .with_context(|| format!("no bond {} for {builder} at {key}", self.bond_index))?;
        let now = Utc::now().timestamp();
        write!(out, "{}", describe(&key, &bond, now))?;
        Ok(())
    }
}

/// The lines `show` prints. Split out so a test can check them without a cluster.
fn describe(key: &Pubkey, bond: &BuilderBond, now: i64) -> String {
    let hold = if bond.hold_started() {
        DateTime::<Utc>::from_timestamp(bond.hold_expires_at, 0)
            .map_or_else(|| bond.hold_expires_at.to_string(), |at| at.to_rfc3339())
    } else {
        "not started: nothing posted yet".to_string()
    };
    let rate = if bond.committed_rate_bits_per_sec == u64::MAX {
        "unmetered".to_string()
    } else {
        format!("{} bits/sec", bond.committed_rate_bits_per_sec)
    };
    format!(
        "Address:      {key}\n\
         Builder:      {}\n\
         Bond index:   {}\n\
         Committed:    {rate}\n\
         Bonded:       {}\n\
         Required:     {}\n\
         Covers:       {}\n\
         Hold expires: {hold}\n\
         Withdrawable: {}\n",
        bond.builder,
        bond.bond_index,
        in_2z(bond.bonded_2z_amount),
        in_2z(bond.required_2z_amount),
        bond.is_funded(),
        in_2z(bond.withdrawable_2z_amount(now)),
    )
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

/// The key `show` reads for when no `--builder` is given: the configured keypair's.
fn own_key(ctx: &CliContext) -> Result<Pubkey> {
    let path = ctx
        .keypair_path
        .as_ref()
        .ok_or_else(|| anyhow!("no keypair configured: pass --builder"))?;
    solana_sdk::signature::read_keypair_file(path)
        .map(|keypair| solana_sdk::signer::Signer::pubkey(&keypair))
        .map_err(|err| anyhow!("reading the keypair at {}: {err}", path.display()))
}

async fn send(
    wallet: &doublezero_solana_client_tools::payer::Wallet,
    instructions: &[Instruction],
    done: &str,
    out: &mut impl Write,
) -> Result<()> {
    let transaction = wallet.new_transaction(instructions).await?;
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
    fn describe_reports_the_hold_and_what_can_be_withdrawn() {
        let builder = Pubkey::new_unique();
        let key = bond_address(&builder, 0);
        // Field by field: the struct's padding is private to the program crate.
        let mut bond = BuilderBond::default();
        bond.builder = builder;
        bond.committed_rate_bits_per_sec = 1_000_000_000;
        bond.required_2z_amount = 100_000_000;

        let unposted = describe(&key, &bond, 0);
        assert!(unposted.contains("Hold expires: not started"), "{unposted}");
        assert!(unposted.contains("Covers:       false"), "{unposted}");

        bond.bonded_2z_amount = 150_000_000;
        bond.hold_expires_at = 1_800_000_000;
        let held = describe(&key, &bond, 1_799_999_999);
        assert!(
            held.contains("Hold expires: 2027-01-15T08:00:00+00:00"),
            "{held}"
        );
        assert!(held.contains("Withdrawable: 0.00000000 2Z"), "{held}");
        assert!(held.contains("Committed:    1000000000 bits/sec"), "{held}");

        let elapsed = describe(&key, &bond, 1_800_000_000);
        assert!(elapsed.contains("Withdrawable: 0.50000000 2Z"), "{elapsed}");

        bond.committed_rate_bits_per_sec = u64::MAX;
        assert!(describe(&key, &bond, 0).contains("Committed:    unmetered"));
    }
}
