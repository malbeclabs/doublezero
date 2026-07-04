use crate::{doublezerocommand::CliCommand, permission::flags::bitmask_to_names};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::{commands::permission::list::ListPermissionCommand, GetGlobalStateCommand};
use doublezero_serviceability::state::{
    feature_flags::{is_feature_enabled, FeatureFlag},
    globalstate::GlobalState,
    permission::{permission_flags, Permission, PermissionStatus},
};
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::{collections::HashMap, io::Write};

/// Permission flags whose instructions call `authorize()`. Enabling
/// `RequirePermissionAccounts` only changes authorization for these flags — every
/// other flag's instructions still gate on `GlobalState` directly, so a Permission
/// account grants nothing there. Keep in sync with the migrated processors (see
/// `PERMISSION.md`).
const MIGRATED_FLAGS: &[(u128, &str)] = &[
    (permission_flags::PERMISSION_ADMIN, "permission-admin"),
    (permission_flags::ACCESS_PASS_ADMIN, "access-pass-admin"),
    (permission_flags::USER_ADMIN, "user-admin"),
    (permission_flags::TOPOLOGY_ADMIN, "topology-admin"),
    (permission_flags::RESOURCE_ADMIN, "resource-admin"),
    (permission_flags::INDEX_ADMIN, "index-admin"),
];

/// Subsystems still gated on `GlobalState` allowlists/authorities (NOT migrated to
/// `authorize()`). A Permission account grants NOTHING for these — removing a key
/// from `foundation_allowlist`/authorities breaks them with no Permission fallback.
/// Static because the enforcement surface lives in the on-chain program, not the CLI.
const NON_MIGRATED_SUBSYSTEMS: &[&str] = &[
    "device (create/update/delete/resume/sethealth, interface/*)",
    "link (create/update/delete/suspend/resume/sethealth)",
    "exchange (create/update/delete/suspend/resume/setdevice)",
    "location (create/update/delete/suspend/resume)",
    "contributor (create/update/delete/suspend/resume)",
    "tenant (create/update/delete/add|remove_administrator/update_payment_status)",
    "multicastgroup (create/update/delete/suspend/reactivate, allowlist publisher|subscriber add/remove)",
    "user (create_core/update)",
    "governance (globalstate/*, globalconfig/set, allowlist/foundation|qa/*)",
];

/// Legacy keys that authorize `flag` today, mirroring
/// `doublezero_serviceability::authorize::check_legacy_any` for the migrated flags.
///
/// IMPORTANT: this MUST stay in sync with `check_legacy_any` (authorize.rs). If the
/// on-chain legacy mapping changes and this does not, the audit will report wrong
/// gaps. See the maintenance note in `smartcontract/programs/CLAUDE.md`.
fn legacy_keys_for_flag(gs: &GlobalState, flag: u128) -> Vec<(Pubkey, &'static str)> {
    let mut keys = Vec::new();
    // Foundation authorizes every migrated flag in legacy mode.
    let foundation_flags = permission_flags::PERMISSION_ADMIN
        | permission_flags::ACCESS_PASS_ADMIN
        | permission_flags::USER_ADMIN
        | permission_flags::TOPOLOGY_ADMIN
        | permission_flags::RESOURCE_ADMIN
        | permission_flags::INDEX_ADMIN;
    if flag & foundation_flags != 0 {
        for pk in &gs.foundation_allowlist {
            keys.push((*pk, "foundation-allowlist"));
        }
    }
    // ACCESS_PASS_ADMIN also honors the sentinel and feed authorities.
    if flag & permission_flags::ACCESS_PASS_ADMIN != 0 {
        if gs.sentinel_authority_pk != Pubkey::default() {
            keys.push((gs.sentinel_authority_pk, "sentinel-authority"));
        }
        if gs.feed_authority_pk != Pubkey::default() {
            keys.push((gs.feed_authority_pk, "feed-authority"));
        }
    }
    keys
}

/// A legacy key that authorizes a migrated instruction today but lacks an equivalent
/// Permission account — i.e. it would lose access if strict mode were enabled.
#[derive(Serialize, Debug, PartialEq)]
struct Gap {
    key: String,
    flag: String,
    legacy_source: String,
}

#[derive(Serialize, Debug)]
struct AuditReport {
    strict_mode_enabled: bool,
    gaps: Vec<Gap>,
    permission_admin_holders: Vec<String>,
    foundation_flag_holders: Vec<String>,
    suspended: Vec<String>,
    unknown_bit_accounts: Vec<String>,
    foundation_allowlist: Vec<String>,
    non_migrated_subsystems: Vec<String>,
}

fn build_report(gs: &GlobalState, permissions: &HashMap<Pubkey, Permission>) -> AuditReport {
    // Index permissions by the pubkey they grant rights to (one per user_payer).
    let by_payer: HashMap<Pubkey, &Permission> =
        permissions.values().map(|p| (p.user_payer, p)).collect();

    let strict_mode_enabled =
        is_feature_enabled(gs.feature_flags, FeatureFlag::RequirePermissionAccounts);

    // Coverage gaps for migrated flags.
    let mut gaps = Vec::new();
    for (flag, flag_name) in MIGRATED_FLAGS {
        for (key, source) in legacy_keys_for_flag(gs, *flag) {
            // Foundation members can always exercise PERMISSION_ADMIN via the
            // foundation recovery path, so they are never at risk for that flag.
            if *flag == permission_flags::PERMISSION_ADMIN && gs.foundation_allowlist.contains(&key)
            {
                continue;
            }
            let covered = by_payer.get(&key).is_some_and(|p| {
                p.status == PermissionStatus::Activated && p.permissions & flag != 0
            });
            if !covered {
                gaps.push(Gap {
                    key: key.to_string(),
                    flag: (*flag_name).to_string(),
                    legacy_source: source.to_string(),
                });
            }
        }
    }
    gaps.sort_by(|a, b| (&a.key, &a.flag).cmp(&(&b.key, &b.flag)));

    // Super-admin holders, suspended accounts, and accounts carrying unknown bits.
    let mut permission_admin_holders = Vec::new();
    let mut foundation_flag_holders = Vec::new();
    let mut suspended = Vec::new();
    let mut unknown_bit_accounts = Vec::new();
    for p in permissions.values() {
        if p.status == PermissionStatus::Suspended {
            suspended.push(p.user_payer.to_string());
        }
        if p.status == PermissionStatus::Activated {
            if p.permissions & permission_flags::PERMISSION_ADMIN != 0 {
                permission_admin_holders.push(p.user_payer.to_string());
            }
            if p.permissions & permission_flags::FOUNDATION != 0 {
                foundation_flag_holders.push(p.user_payer.to_string());
            }
        }
        // Set bits with no known name (count of set bits exceeds named bits).
        if p.permissions.count_ones() as usize > bitmask_to_names(p.permissions).len() {
            unknown_bit_accounts.push(p.user_payer.to_string());
        }
    }
    permission_admin_holders.sort();
    foundation_flag_holders.sort();
    suspended.sort();
    unknown_bit_accounts.sort();

    let mut foundation_allowlist: Vec<String> = gs
        .foundation_allowlist
        .iter()
        .map(|k| k.to_string())
        .collect();
    foundation_allowlist.sort();

    AuditReport {
        strict_mode_enabled,
        gaps,
        permission_admin_holders,
        foundation_flag_holders,
        suspended,
        unknown_bit_accounts,
        foundation_allowlist,
        non_migrated_subsystems: NON_MIGRATED_SUBSYSTEMS
            .iter()
            .map(|s| s.to_string())
            .collect(),
    }
}

fn render_text<W: Write>(out: &mut W, report: &AuditReport) -> eyre::Result<()> {
    writeln!(out, "Permission Activation Audit")?;
    writeln!(out, "===========================")?;
    writeln!(out)?;
    writeln!(
        out,
        "Strict mode (require-permission-accounts): {}",
        if report.strict_mode_enabled {
            "ON"
        } else {
            "OFF"
        }
    )?;
    if report.strict_mode_enabled {
        writeln!(
            out,
            "  Gaps below indicate keys ALREADY locked out of migrated instructions."
        )?;
    } else {
        writeln!(
            out,
            "  Gaps below indicate keys that would LOSE access to migrated instructions"
        )?;
        writeln!(out, "  if the flag were enabled now.")?;
    }
    writeln!(out)?;

    writeln!(
        out,
        "Legacy keys missing Permission coverage: {}",
        report.gaps.len()
    )?;
    for g in &report.gaps {
        writeln!(out, "  {}  flag={}  via {}", g.key, g.flag, g.legacy_source)?;
    }
    writeln!(out)?;

    writeln!(
        out,
        "Super-admin review (PERMISSION_ADMIN can grant ANY flag, incl. FOUNDATION):"
    )?;
    writeln!(
        out,
        "  permission-admin holders: {}",
        fmt_list(&report.permission_admin_holders)
    )?;
    writeln!(
        out,
        "  foundation-flag holders:  {}",
        fmt_list(&report.foundation_flag_holders)
    )?;
    writeln!(out)?;

    if !report.suspended.is_empty() {
        writeln!(
            out,
            "Suspended Permission accounts: {}",
            fmt_list(&report.suspended)
        )?;
    }
    if !report.unknown_bit_accounts.is_empty() {
        writeln!(
            out,
            "Permission accounts with unknown bits: {}",
            fmt_list(&report.unknown_bit_accounts)
        )?;
    }
    writeln!(out)?;

    writeln!(
        out,
        "Legacy dependency warning — DO NOT remove these keys yet"
    )?;
    writeln!(
        out,
        "-------------------------------------------------------"
    )?;
    writeln!(
        out,
        "The following subsystems are NOT migrated to the Permission model; they still"
    )?;
    writeln!(
        out,
        "authorize via GlobalState allowlists/authorities. A Permission account grants"
    )?;
    writeln!(
        out,
        "nothing for them, so removing a key from foundation_allowlist/authorities breaks"
    )?;
    writeln!(out, "these with no fallback:")?;
    for s in &report.non_migrated_subsystems {
        writeln!(out, "  - {s}")?;
    }
    writeln!(out)?;
    writeln!(
        out,
        "foundation_allowlist ({} member(s)): {}",
        report.foundation_allowlist.len(),
        fmt_list(&report.foundation_allowlist)
    )?;
    writeln!(out)?;

    if report.gaps.is_empty() {
        writeln!(out, "Result: no coverage gaps for migrated instructions.")?;
    } else {
        writeln!(
            out,
            "Result: {} gap(s) — provisioning incomplete.",
            report.gaps.len()
        )?;
    }
    Ok(())
}

fn fmt_list(items: &[String]) -> String {
    if items.is_empty() {
        "(none)".to_string()
    } else {
        items.join(", ")
    }
}

#[derive(Args, Debug, Default)]
pub struct AuditPermissionCliCommand {
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
    /// Output as compact JSON
    #[arg(long, default_value_t = false)]
    pub json_compact: bool,
}

impl AuditPermissionCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let (_, globalstate) = client.get_globalstate(GetGlobalStateCommand)?;
        let permissions = client.list_permission(ListPermissionCommand {})?;

        let report = build_report(&globalstate, &permissions);

        if self.json || self.json_compact {
            let s = if self.json_compact {
                serde_json::to_string(&report)?
            } else {
                serde_json::to_string_pretty(&report)?
            };
            writeln!(out, "{s}")?;
        } else {
            render_text(out, &report)?;
        }

        // Non-zero exit when provisioning is incomplete, so this is usable as a
        // pre-activation gate in a runbook / CI.
        if !report.gaps.is_empty() {
            return Err(eyre::eyre!(
                "{} permission gap(s) found — do not enable require-permission-accounts until \
                 every legacy key has an equivalent Permission account",
                report.gaps.len()
            ));
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::AccountType;
    use mockall::predicate;

    fn globalstate_with_foundation(members: Vec<Pubkey>, feature_flags: u128) -> GlobalState {
        GlobalState {
            foundation_allowlist: members,
            feature_flags,
            ..Default::default()
        }
    }

    fn permission(user_payer: Pubkey, permissions: u128, status: PermissionStatus) -> Permission {
        Permission {
            account_type: AccountType::Permission,
            owner: Pubkey::new_unique(),
            bump_seed: 255,
            status,
            user_payer,
            permissions,
        }
    }

    #[test]
    fn test_legacy_keys_for_access_pass_admin_includes_sentinel_and_feed() {
        let sentinel = Pubkey::new_unique();
        let feed = Pubkey::new_unique();
        let foundation = Pubkey::new_unique();
        let mut gs = globalstate_with_foundation(vec![foundation], 0);
        gs.sentinel_authority_pk = sentinel;
        gs.feed_authority_pk = feed;

        let keys = legacy_keys_for_flag(&gs, permission_flags::ACCESS_PASS_ADMIN);
        let pks: Vec<Pubkey> = keys.iter().map(|(k, _)| *k).collect();
        assert!(pks.contains(&foundation));
        assert!(pks.contains(&sentinel));
        assert!(pks.contains(&feed));
    }

    #[test]
    fn test_foundation_not_flagged_for_permission_admin() {
        // A foundation member with no Permission account is safe for PERMISSION_ADMIN
        // (recovery path) but IS a gap for the other migrated flags.
        let foundation = Pubkey::new_unique();
        let gs = globalstate_with_foundation(vec![foundation], 0);
        let permissions = HashMap::new();

        let report = build_report(&gs, &permissions);
        assert!(!report.gaps.iter().any(|g| g.flag == "permission-admin"));
        // user-admin, topology-admin, resource-admin, index-admin, access-pass-admin
        assert_eq!(report.gaps.len(), 5);
    }

    #[test]
    fn test_no_gaps_when_foundation_fully_provisioned() {
        let foundation = Pubkey::new_unique();
        let gs = globalstate_with_foundation(vec![foundation], 0);
        // Grant every migrated non-recovery flag.
        let mask = permission_flags::USER_ADMIN
            | permission_flags::TOPOLOGY_ADMIN
            | permission_flags::RESOURCE_ADMIN
            | permission_flags::INDEX_ADMIN
            | permission_flags::ACCESS_PASS_ADMIN;
        let pda = Pubkey::new_unique();
        let permissions = HashMap::from([(
            pda,
            permission(foundation, mask, PermissionStatus::Activated),
        )]);

        let report = build_report(&gs, &permissions);
        assert!(report.gaps.is_empty(), "unexpected gaps: {:?}", report.gaps);
    }

    #[test]
    fn test_suspended_permission_does_not_cover() {
        let foundation = Pubkey::new_unique();
        let gs = globalstate_with_foundation(vec![foundation], 0);
        let mask = permission_flags::USER_ADMIN
            | permission_flags::TOPOLOGY_ADMIN
            | permission_flags::RESOURCE_ADMIN
            | permission_flags::INDEX_ADMIN
            | permission_flags::ACCESS_PASS_ADMIN;
        let pda = Pubkey::new_unique();
        let permissions = HashMap::from([(
            pda,
            permission(foundation, mask, PermissionStatus::Suspended),
        )]);

        let report = build_report(&gs, &permissions);
        // Suspended → still a gap for all 5 non-recovery flags.
        assert_eq!(report.gaps.len(), 5);
        assert_eq!(report.suspended, vec![foundation.to_string()]);
    }

    #[test]
    fn test_superadmin_holders_reported() {
        let holder = Pubkey::new_unique();
        let gs = globalstate_with_foundation(vec![], 0);
        let pda = Pubkey::new_unique();
        let permissions = HashMap::from([(
            pda,
            permission(
                holder,
                permission_flags::PERMISSION_ADMIN | permission_flags::FOUNDATION,
                PermissionStatus::Activated,
            ),
        )]);

        let report = build_report(&gs, &permissions);
        assert_eq!(report.permission_admin_holders, vec![holder.to_string()]);
        assert_eq!(report.foundation_flag_holders, vec![holder.to_string()]);
    }

    #[test]
    fn test_cli_permission_audit_reports_gap_and_errors() {
        let mut client = create_test_client();
        let foundation = Pubkey::new_unique();
        let gs = globalstate_with_foundation(vec![foundation], 0);
        let gstate_pubkey = Pubkey::new_unique();

        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, gs.clone())));
        client
            .expect_list_permission()
            .returning(|_| Ok(HashMap::new()));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let cmd = AuditPermissionCliCommand::default();
        let res = block_on(cmd.execute(&ctx, &client, &mut output));

        assert!(res.is_err(), "expected non-zero exit on gaps");
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("Legacy keys missing Permission coverage: 5"));
        assert!(output_str.contains("DO NOT remove these keys yet"));
    }
}
