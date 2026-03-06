use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::{
    commands::{accesspass::list::ListAccessPassCommand, user::list::ListUserCommand},
    UserType,
};
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, collections::HashSet, io::{BufRead, Write}};

const USER_RENT_BYTES: usize = 240;
const GAS_FEE_RESERVE: u64 = 50 * 5_000;

#[derive(Args, Debug, Default)]
pub struct FundAccessPassCliCommand {
    /// Only fund this specific user payer
    #[arg(long)]
    pub user_payer: Option<Pubkey>,
    /// Minimum balance in SOL each payer should hold (in addition to rent)
    #[arg(long)]
    pub min_balance: Option<f64>,
    /// Dry run: print what would be transferred without sending
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,
    /// Skip confirmation prompt and transfer immediately
    #[arg(long, default_value_t = false)]
    pub force: bool,
}

impl FundAccessPassCliCommand {
    pub fn execute<C: CliCommand, W: Write, R: BufRead>(
        self,
        client: &C,
        out: &mut W,
        input: &mut R,
    ) -> eyre::Result<()> {
        let access_passes = client.list_accesspass(ListAccessPassCommand)?;
        let users = client.list_user(ListUserCommand {})?;
        let rent_per_user = client.get_minimum_balance_for_rent_exemption(USER_RENT_BYTES)?;

        // Count effective slots per payer: +1 per IBRL access pass, +1 per multicast-enabled access pass
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
        let min_balance_lamports = self
            .min_balance
            .map(|sol| (sol * 1_000_000_000.0) as u64)
            .unwrap_or(0);
        // A wallet account (0 data bytes) must hold at least this many lamports
        // or Solana rejects the transfer with "insufficient funds for rent".
        let wallet_rent_min = client.get_minimum_balance_for_rent_exemption(0)?;

        let to_fund: Vec<(Pubkey, u64)> = unique_payers
            .into_iter()
            .zip(balances)
            .filter_map(|(user_payer, account)| {
                let lamports = account.map(|a| a.lamports).unwrap_or(0);
                let access_passes = ap_count_by_payer.get(&user_payer).copied().unwrap_or(0);
                let connected = unicast_by_payer.get(&user_payer).copied().unwrap_or(0) as usize
                    + multicast_by_payer.get(&user_payer).copied().unwrap_or(0) as usize;
                let remaining_slots = access_passes.saturating_sub(connected);
                let needs_rent = rent_per_user.saturating_mul(remaining_slots as u64) + GAS_FEE_RESERVE;
                let required = needs_rent.max(min_balance_lamports).max(wallet_rent_min);
                let deficit = required.saturating_sub(lamports);
                if deficit > 0 {
                    Some((user_payer, deficit))
                } else {
                    None
                }
            })
            .collect();

        if to_fund.is_empty() {
            writeln!(out, "All user payers are sufficiently funded.")?;
            return Ok(());
        }

        let total_lamports: u64 = to_fund.iter().map(|(_, d)| d).sum();
        let total_sol = total_lamports as f64 / 1_000_000_000.0;

        let sender = client.get_payer();
        let sender_balance = client.get_balance()?;
        let sender_sol = sender_balance as f64 / 1_000_000_000.0;

        writeln!(out, "Transfers to execute:")?;
        for (i, (user_payer, deficit)) in to_fund.iter().enumerate() {
            let sol = *deficit as f64 / 1_000_000_000.0;
            writeln!(out, "  {:>3}. {user_payer}  {sol:.9} SOL", i + 1)?;
        }
        if sender_balance < total_lamports {
            let shortfall = (total_lamports - sender_balance) as f64 / 1_000_000_000.0;
            writeln!(
                out,
                "Total:  {total_sol:.9} SOL  (sender {sender}: {sender_sol:.9} SOL — WARNING: insufficient, short {shortfall:.9} SOL)"
            )?;
        } else {
            writeln!(
                out,
                "Total:  {total_sol:.9} SOL  (sender {sender}: {sender_sol:.9} SOL)"
            )?;
        }

        if self.dry_run {
            writeln!(out, "[dry-run] no transfers sent.")?;
            return Ok(());
        }

        if !self.force {
            write!(out, "Proceed? [y/N]: ")?;
            out.flush()?;
            let mut answer = String::new();
            input.read_line(&mut answer)?;
            if !answer.trim().eq_ignore_ascii_case("y") {
                writeln!(out, "Aborted.")?;
                return Ok(());
            }
        }

        for (user_payer, deficit) in to_fund {
            let sol = deficit as f64 / 1_000_000_000.0;
            let signature = client.transfer_sol(user_payer, deficit)?;
            writeln!(out, "transferred {sol:.9} SOL to {user_payer} (sig: {signature})")?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_sdk::AccountType;
    use doublezero_serviceability::state::accesspass::{
        AccessPass, AccessPassStatus, AccessPassType,
    };
    use solana_sdk::{account::Account, pubkey::Pubkey};

    const RENT_PER_USER: u64 = 1_000_000;
    // needs_rent for 1 remaining slot = 1_000_000 + 250_000 = 1_250_000

    fn make_ibrl_access_pass(user_payer: Pubkey) -> AccessPass {
        AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 1,
            client_ip: "1.2.3.4".parse().unwrap(),
            accesspass_type: AccessPassType::Prepaid,
            user_payer,
            last_access_epoch: 1,
            owner: user_payer,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            flags: 0,
        }
    }

    fn setup_client_with_balance(
        payer: Pubkey,
        balance: u64,
    ) -> crate::doublezerocommand::MockCliCommand {
        let mut client = create_test_client();
        let ap_key = Pubkey::new_unique();
        let ap = make_ibrl_access_pass(payer);

        client.expect_list_accesspass().returning(move |_| {
            let mut map = HashMap::new();
            map.insert(ap_key, ap.clone());
            Ok(map)
        });
        client
            .expect_list_user()
            .returning(|_| Ok(HashMap::new()));
        client
            .expect_get_minimum_balance_for_rent_exemption()
            .returning(|_| Ok(RENT_PER_USER));
        client
            .expect_get_multiple_accounts()
            .returning(move |_| Ok(vec![Some(Account { lamports: balance, ..Account::default() })]));
        client
    }

    #[test]
    fn test_fund_all_sufficiently_funded() {
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        // balance > needs_rent (1_250_000)
        let client = setup_client_with_balance(payer, 2_000_000);

        let mut out = Vec::new();
        let res = FundAccessPassCliCommand::default().execute(&client, &mut out, &mut "".as_bytes());

        assert!(res.is_ok());
        assert_eq!(
            String::from_utf8(out).unwrap(),
            "All user payers are sufficiently funded.\n"
        );
    }

    #[test]
    fn test_fund_dry_run_shows_summary_without_transferring() {
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        // balance = 500_000 < needs_rent (1_250_000), deficit = 750_000
        let client = setup_client_with_balance(payer, 500_000);

        let mut out = Vec::new();
        let res = FundAccessPassCliCommand {
            dry_run: true,
            ..Default::default()
        }
        .execute(&client, &mut out, &mut "".as_bytes());

        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("Transfers to execute:"));
        assert!(output.contains(&payer.to_string()));
        assert!(output.contains("0.000750000 SOL"));
        assert!(output.contains("Total:"));
        assert!(output.contains("[dry-run] no transfers sent."));
    }

    #[test]
    fn test_fund_confirmation_yes_transfers() {
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let mut client = setup_client_with_balance(payer, 500_000);
        client
            .expect_transfer_sol()
            .returning(|_, _| Ok(solana_sdk::signature::Signature::default()));

        let mut out = Vec::new();
        let res = FundAccessPassCliCommand::default()
            .execute(&client, &mut out, &mut "y\n".as_bytes());

        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("Proceed? [y/N]:"));
        assert!(output.contains("transferred"));
    }

    #[test]
    fn test_fund_confirmation_no_aborts() {
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let client = setup_client_with_balance(payer, 500_000);

        let mut out = Vec::new();
        let res = FundAccessPassCliCommand::default()
            .execute(&client, &mut out, &mut "n\n".as_bytes());

        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("Proceed? [y/N]:"));
        assert!(output.contains("Aborted."));
        assert!(!output.contains("transferred"));
    }

    #[test]
    fn test_fund_min_balance_dominates_rent() {
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        // balance = 1_500_000 > needs_rent (1_250_000) but < min_balance (2_000_000)
        // required = max(1_250_000, 2_000_000) = 2_000_000, deficit = 500_000
        let client = setup_client_with_balance(payer, 1_500_000);

        let mut out = Vec::new();
        let res = FundAccessPassCliCommand {
            min_balance: Some(0.002), // 2_000_000 lamports
            dry_run: true,
            ..Default::default()
        }
        .execute(&client, &mut out, &mut "".as_bytes());

        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("0.000500000 SOL"));
        assert!(output.contains(&payer.to_string()));
    }

    #[test]
    fn test_fund_rent_dominates_min_balance() {
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        // balance = 500_000, min_balance = ~1 lamport
        // required = max(1_250_000, 1) = 1_250_000, deficit = 750_000
        let client = setup_client_with_balance(payer, 500_000);

        let mut out = Vec::new();
        let res = FundAccessPassCliCommand {
            min_balance: Some(0.000000001),
            dry_run: true,
            ..Default::default()
        }
        .execute(&client, &mut out, &mut "".as_bytes());

        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(output.contains("0.000750000 SOL"));
        assert!(output.contains(&payer.to_string()));
    }

    #[test]
    fn test_fund_force_skips_confirmation() {
        let payer = Pubkey::from_str_const("1111111FVAiSujNZVgYSc27t6zUTWoKfAGxbRzzPB");
        let mut client = setup_client_with_balance(payer, 500_000);
        client
            .expect_transfer_sol()
            .returning(|_, _| Ok(solana_sdk::signature::Signature::default()));

        let mut out = Vec::new();
        let res = FundAccessPassCliCommand {
            force: true,
            ..Default::default()
        }
        .execute(&client, &mut out, &mut "".as_bytes());

        assert!(res.is_ok());
        let output = String::from_utf8(out).unwrap();
        assert!(!output.contains("Proceed? [y/N]:"));
        assert!(output.contains("transferred"));
    }
}
