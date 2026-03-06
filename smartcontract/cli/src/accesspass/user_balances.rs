use crate::doublezerocommand::CliCommand;
use clap::{Args, ValueEnum};
use console::style;
use doublezero_program_common::serializer;
use doublezero_sdk::{
    commands::{accesspass::list::ListAccessPassCommand, user::list::ListUserCommand},
    UserType,
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::fmt;
use std::{collections::HashMap, collections::HashSet, io::Write};

use tabled::{
    settings::{object::Columns, Alignment, Modify, Style},
    Table, Tabled,
};

// Size of an user account
const USER_RENT_BYTES: usize = 240;
const GAS_FEE_RESERVE: u64 = 50 * 5_000;

#[derive(Default, Serialize)]
pub struct Lamports(pub u64);

impl fmt::Display for Lamports {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{:.9}", self.0 as f64 / 1_000_000_000.0)
    }
}

#[derive(ValueEnum, Clone, Debug, Default)]
pub enum SortColumn {
    #[default]
    Balance,
    AccessPasses,
    Unicast,
    Multicast,
    Required,
    Missing,
}

#[derive(ValueEnum, Clone, Debug, Default)]
pub enum SortOrder {
    Asc,
    #[default]
    Desc,
}

#[derive(Args, Debug, Default)]
pub struct UserBalancesAccessPassCliCommand {
    /// Filter by user payer public key
    #[arg(long)]
    pub user_payer: Option<Pubkey>,
    /// Minimum balance in SOL (inclusive)
    #[arg(long)]
    pub min_balance: Option<f64>,
    /// Maximum balance in SOL (inclusive)
    #[arg(long)]
    pub max_balance: Option<f64>,
    /// Minimum missing amount in SOL (inclusive)
    #[arg(long)]
    pub min_missing: Option<f64>,
    /// Maximum missing amount in SOL (inclusive)
    #[arg(long)]
    pub max_missing: Option<f64>,
    /// Column to sort by
    #[arg(long, default_value = "balance")]
    pub sort_by: SortColumn,
    /// Sort order
    #[arg(long, default_value = "asc")]
    pub sort_order: SortOrder,
    /// Limit results to the top N rows
    #[arg(long)]
    pub top: Option<usize>,
}

#[derive(Tabled, Serialize)]
pub struct UserBalanceDisplay {
    #[tabled(rename = "#")]
    pub index: usize,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub user_payer: Pubkey,
    pub access_passes: usize,
    #[tabled(rename = "connected_unicast")]
    pub unicast: u32,
    #[tabled(rename = "connected_multicast")]
    pub multicast: u32,
    pub balance: Lamports,
    pub required: Lamports,
    pub missing: Lamports,
}

impl UserBalancesAccessPassCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let access_passes = client.list_accesspass(ListAccessPassCommand)?;
        let users = client.list_user(ListUserCommand {})?;
        let rent_per_ip = client.get_minimum_balance_for_rent_exemption(USER_RENT_BYTES)?;

        // Each access pass contributes +1 per active IBRL (last_access_epoch > 0)
        // and +1 per access pass with at least one multicast group.
        let mut ap_count_by_payer: HashMap<Pubkey, usize> = HashMap::new();
        for ap in access_passes.values() {
            let count = (ap.last_access_epoch > 0) as usize
                + (!ap.mgroup_pub_allowlist.is_empty() || !ap.mgroup_sub_allowlist.is_empty())
                    as usize;
            *ap_count_by_payer.entry(ap.user_payer).or_default() += count;
        }

        let mut unicast_by_payer: HashMap<Pubkey, u32> = HashMap::new();
        let mut multicast_by_payer: HashMap<Pubkey, u32> = HashMap::new();
        for user in users.values() {
            match user.user_type {
                UserType::IBRL | UserType::IBRLWithAllocatedIP => {
                    *unicast_by_payer.entry(user.owner).or_default() += 1;
                }
                UserType::Multicast => {
                    *multicast_by_payer.entry(user.owner).or_default() += 1;
                }
                UserType::EdgeFiltering => {}
            }
        }

        let mut seen = HashSet::new();
        let mut unique_payers: Vec<Pubkey> = access_passes
            .values()
            .filter_map(|ap| {
                if seen.insert(ap.user_payer) {
                    Some(ap.user_payer)
                } else {
                    None
                }
            })
            .collect();
        unique_payers.sort();

        if let Some(filter) = self.user_payer {
            unique_payers.retain(|p| *p == filter);
        }

        let balances = client.get_multiple_accounts(unique_payers.clone())?;
        let wallet_rent_min = client.get_minimum_balance_for_rent_exemption(0)?;

        let mut rows: Vec<(u64, UserBalanceDisplay)> = unique_payers
            .into_iter()
            .zip(balances)
            .map(|(user_payer, account)| {
                let lamports = account.map(|a| a.lamports).unwrap_or(0);
                let access_passes = ap_count_by_payer.get(&user_payer).copied().unwrap_or(0);
                let unicast = unicast_by_payer.get(&user_payer).copied().unwrap_or(0);
                let multicast = multicast_by_payer.get(&user_payer).copied().unwrap_or(0);
                let connected = unicast as usize + multicast as usize;
                let remaining_slots = access_passes.saturating_sub(connected);
                let needs_rent =
                    rent_per_ip.saturating_mul(remaining_slots as u64) + GAS_FEE_RESERVE;
                let required = needs_rent.max(wallet_rent_min);
                let deficit = required.saturating_sub(lamports);
                (
                    lamports,
                    UserBalanceDisplay {
                        index: 0,
                        access_passes,
                        unicast,
                        multicast,
                        user_payer,
                        balance: Lamports(lamports),
                        required: Lamports(required),
                        missing: Lamports(deficit),
                    },
                )
            })
            .collect();
        rows.sort_by(|(la, a), (lb, b)| {
            let ord = match self.sort_by {
                SortColumn::Balance => la.cmp(lb),
                SortColumn::AccessPasses => a.access_passes.cmp(&b.access_passes),
                SortColumn::Unicast => a.unicast.cmp(&b.unicast),
                SortColumn::Multicast => a.multicast.cmp(&b.multicast),
                SortColumn::Required => a.required.0.cmp(&b.required.0),
                SortColumn::Missing => a.missing.0.cmp(&b.missing.0),
            };
            if matches!(self.sort_order, SortOrder::Desc) {
                ord.reverse()
            } else {
                ord
            }
        });

        let min_lamports = self.min_balance.map(|sol| (sol * 1_000_000_000.0) as u64);
        let max_lamports = self.max_balance.map(|sol| (sol * 1_000_000_000.0) as u64);
        let min_missing_lamports = self.min_missing.map(|sol| (sol * 1_000_000_000.0) as u64);
        let max_missing_lamports = self.max_missing.map(|sol| (sol * 1_000_000_000.0) as u64);

        let top = self.top;
        let (red_indices, display_rows): (HashSet<usize>, Vec<UserBalanceDisplay>) = rows
            .into_iter()
            .filter(|(lamports, row)| {
                min_lamports.map_or(true, |min| *lamports >= min)
                    && max_lamports.map_or(true, |max| *lamports <= max)
                    && min_missing_lamports.map_or(true, |min| row.missing.0 >= min)
                    && max_missing_lamports.map_or(true, |max| row.missing.0 <= max)
            })
            .take(top.unwrap_or(usize::MAX))
            .enumerate()
            .map(|(i, (_, mut row))| {
                row.index = i + 1;
                let is_red = row.missing.0 > 0;
                (is_red, row)
            })
            .fold(
                (HashSet::new(), Vec::new()),
                |(mut reds, mut rows), (is_red, row)| {
                    if is_red {
                        reds.insert(rows.len());
                    }
                    rows.push(row);
                    (reds, rows)
                },
            );

        let table = Table::new(display_rows)
            .with(Style::psql().remove_horizontals())
            .with(Modify::new(Columns::new(0..=0)).with(Alignment::right()))
            .with(Modify::new(Columns::new(2..=2)).with(Alignment::right()))
            .with(Modify::new(Columns::new(3..=3)).with(Alignment::right()))
            .with(Modify::new(Columns::new(4..=4)).with(Alignment::right()))
            .with(Modify::new(Columns::new(5..=5)).with(Alignment::right()))
            .with(Modify::new(Columns::new(6..=6)).with(Alignment::right()))
            .with(Modify::new(Columns::new(7..=7)).with(Alignment::right()))
            .to_string();

        // Color rows after rendering to avoid ANSI codes breaking column widths.
        // Line 0 is the header; data rows start at line 1.
        for (i, line) in table.lines().enumerate() {
            if i > 0 && red_indices.contains(&(i - 1)) {
                writeln!(out, "{}", style(line).red())?;
            } else {
                writeln!(out, "{line}")?;
            }
        }

        Ok(())
    }
}
