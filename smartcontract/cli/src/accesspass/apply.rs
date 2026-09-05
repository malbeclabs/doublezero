//! Converges the ledger onto an access-pass definition document.
//!
//! Builds the same plan `plan` prints, shows it, asks for confirmation, then sends each change
//! and reports the outcome per item. Each allowlist change is its own instruction and its own
//! transaction — there is no multi-group allowlist instruction — so a run continues past a
//! failure and names what did and did not land.

use crate::{
    accesspass::{
        desired::AccessPassDocument,
        plan::{build_plan, render_plan, AccessPassPlan, IbrlChange, Op, PlannedChange, Role},
    },
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::{
    accesspass::{get::GetAccessPassCommand, set::SetAccessPassCommand},
    multicastgroup::allowlist::{
        publisher::{
            add::AddMulticastGroupPubAllowlistCommand,
            remove::RemoveMulticastGroupPubAllowlistCommand,
        },
        subscriber::{
            add::AddMulticastGroupSubAllowlistCommand,
            remove::RemoveMulticastGroupSubAllowlistCommand,
        },
    },
};
use serde::Serialize;
use std::{
    io::{BufRead, Write},
    path::PathBuf,
};

/// Reads an access-pass definition document and converges the ledger onto it.
#[derive(Args, Debug)]
pub struct ApplyAccessPassCliCommand {
    /// Path to the access-pass definition document (YAML)
    #[arg(long, short = 'f')]
    pub file: PathBuf,
    /// Show the plan and exit without writing anything
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,
    /// Skip the confirmation prompt
    #[arg(long, alias = "force", default_value_t = false)]
    pub auto_approve: bool,
    /// Also list the grants that are already satisfied
    #[arg(long, default_value_t = false)]
    pub verbose: bool,
    /// Output as pretty JSON
    #[arg(long, default_value_t = false)]
    pub json: bool,
}

#[derive(Debug, Serialize)]
struct ApplyResult {
    #[serde(flatten)]
    change: PlannedChange,
    /// `applied` or `failed`.
    state: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    signature: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
}

#[derive(Debug, Serialize)]
struct IbrlResult {
    #[serde(flatten)]
    change: IbrlChange,
    /// `applied` or `failed`.
    state: &'static str,
    #[serde(skip_serializing_if = "Option::is_none")]
    signature: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
}

#[derive(Debug, Serialize)]
struct ApplyJson<'a> {
    changed: bool,
    counts: Counts,
    results: &'a [ApplyResult],
    ibrl_results: &'a [IbrlResult],
    plan: &'a AccessPassPlan,
}

#[derive(Debug, Serialize)]
struct Counts {
    applied: usize,
    failed: usize,
    satisfied: usize,
    blocked: usize,
}

impl ApplyAccessPassCliCommand {
    pub async fn execute<C: CliCommand, W: Write, R: BufRead>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
        input: &mut R,
    ) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        // A JSON consumer has no terminal to answer the prompt on, so make the caller say up
        // front that no one is watching rather than hanging on a read that never returns.
        if self.json && !self.dry_run && !self.auto_approve {
            eyre::bail!(
                "--json requires --auto-approve (or --dry-run); there is no way to confirm"
            );
        }

        let document = AccessPassDocument::from_path(&self.file)?;
        let desired = document.resolve(client.get_payer())?;
        let plan = build_plan(client, &desired)?;

        if !self.json {
            render_plan(out, &plan, self.verbose)?;
        }

        if self.dry_run {
            if self.json {
                emit_json(out, &plan, &[], &[])?;
            } else {
                writeln!(out, "\n[dry-run] nothing was sent.")?;
            }
            return blocked_result(&plan);
        }

        if plan.is_empty() {
            if self.json {
                emit_json(out, &plan, &[], &[])?;
            }
            return blocked_result(&plan);
        }

        if !self.auto_approve {
            write!(out, "\nDo you want to perform these actions? [y/N]: ")?;
            out.flush()?;
            let mut answer = String::new();
            input.read_line(&mut answer)?;
            if !matches!(answer.trim().to_lowercase().as_str(), "y" | "yes") {
                writeln!(out, "Aborted.")?;
                return Ok(());
            }
            writeln!(out)?;
        }

        let mut results = Vec::with_capacity(plan.changes.len());
        let mut ibrl_results: Vec<IbrlResult> = Vec::with_capacity(plan.ibrl_changes.len());
        for change in &plan.changes {
            let outcome = send(client, change);
            match outcome {
                Ok(signature) => {
                    if !self.json {
                        let sign = if change.op == Op::Grant { '+' } else { '-' };
                        writeln!(
                            out,
                            "  {sign} {} {} {}  {signature}  ✓",
                            change.role.label(),
                            change.group,
                            change.client_ip
                        )?;
                    }
                    results.push(ApplyResult {
                        change: change.clone(),
                        state: "applied",
                        signature: Some(signature),
                        error: None,
                    });
                }
                Err(err) => {
                    if !self.json {
                        let sign = if change.op == Op::Grant { '+' } else { '-' };
                        writeln!(
                            out,
                            "  {sign} {} {} {}  {err}  ✗",
                            change.role.label(),
                            change.group,
                            change.client_ip
                        )?;
                    }
                    results.push(ApplyResult {
                        change: change.clone(),
                        state: "failed",
                        signature: None,
                        error: Some(err.to_string()),
                    });
                }
            }
        }

        for change in &plan.ibrl_changes {
            let label = match (&change.from, &change.to) {
                (None, Some(to)) => format!("+ ibrl {to}"),
                (Some(from), None) => format!("- ibrl {from}"),
                (Some(from), Some(to)) => format!("~ ibrl {from} -> {to}"),
                (None, None) => "~ ibrl (epoch only)".to_string(),
            };
            match send_ibrl(client, change) {
                Ok(signature) => {
                    if !self.json {
                        writeln!(out, "  {label} {}  {signature}  ✓", change.client_ip)?;
                    }
                    ibrl_results.push(IbrlResult {
                        change: change.clone(),
                        state: "applied",
                        signature: Some(signature),
                        error: None,
                    });
                }
                Err(err) => {
                    if !self.json {
                        writeln!(out, "  {label} {}  {err}  ✗", change.client_ip)?;
                    }
                    ibrl_results.push(IbrlResult {
                        change: change.clone(),
                        state: "failed",
                        signature: None,
                        error: Some(err.to_string()),
                    });
                }
            }
        }

        let applied = results.iter().filter(|r| r.state == "applied").count()
            + ibrl_results.iter().filter(|r| r.state == "applied").count();
        let failed = results.len() + ibrl_results.len() - applied;

        if self.json {
            emit_json(out, &plan, &results, &ibrl_results)?;
        } else {
            writeln!(out, "\nApply complete! {applied} applied, {failed} failed.")?;
        }

        if failed > 0 {
            eyre::bail!("{failed} of {} allowlist changes failed", results.len());
        }
        blocked_result(&plan)
    }
}

/// The blocked items are the part of the document this run deliberately did not do, so the exit
/// code has to say so even when everything else succeeded.
fn blocked_result(plan: &AccessPassPlan) -> eyre::Result<()> {
    if plan.blocked.is_empty() {
        return Ok(());
    }
    eyre::bail!(
        "{} declared access pass(es) could not be reconciled; see the blocked items above",
        plan.blocked.len()
    )
}

fn send<C: CliCommand>(client: &C, change: &PlannedChange) -> eyre::Result<String> {
    let code = change.group.clone();
    let client_ip = change.client_ip;
    let user_payer = change.user_payer;

    let signature = match (change.role, change.op) {
        (Role::Publisher, Op::Grant) => {
            client.add_multicastgroup_pub_allowlist(AddMulticastGroupPubAllowlistCommand {
                pubkey_or_code: code,
                client_ip,
                user_payer,
            })?
        }
        (Role::Publisher, Op::Revoke) => {
            client.remove_multicastgroup_pub_allowlist(RemoveMulticastGroupPubAllowlistCommand {
                pubkey_or_code: code,
                client_ip,
                user_payer,
            })?
        }
        (Role::Subscriber, Op::Grant) => {
            client.add_multicastgroup_sub_allowlist(AddMulticastGroupSubAllowlistCommand {
                pubkey_or_code: code,
                client_ip,
                user_payer,
            })?
        }
        (Role::Subscriber, Op::Revoke) => {
            client.remove_multicastgroup_sub_allowlist(RemoveMulticastGroupSubAllowlistCommand {
                pubkey_or_code: code,
                client_ip,
                user_payer,
            })?
        }
        // IBRL is not an allowlist entry; it is written by `access-pass set` through
        // `send_ibrl`, and the planner never puts it in `changes`.
        (Role::Ibrl, _) => {
            eyre::bail!("internal error: an IBRL change reached the allowlist writer")
        }
    };

    Ok(signature.to_string())
}

/// Re-sends `access-pass set` with the tenant the document declares.
///
/// `set` overwrites `accesspass_type`, `last_access_epoch`, the `ALLOW_MULTIPLE_IP` flag and both
/// seat caps from its arguments, so the pass is read back and those are sent unchanged — the write
/// moves the tenant, pins the epoch, and touches nothing else. (`mgroup_*_allowlist` and
/// `DZF_LOCKED` survive a `set` untouched; EdgeSeat feed seats are preserved by the program when
/// both the stored and incoming types are EdgeSeat.)
///
/// The read is deliberately fresh rather than carried from the plan: the seat caps are live
/// counters that the plan may already have seen go stale.
fn send_ibrl<C: CliCommand>(client: &C, change: &IbrlChange) -> eyre::Result<String> {
    let (_, pass) = client
        .get_accesspass(GetAccessPassCommand {
            client_ip: change.client_ip,
            user_payer: change.user_payer,
        })?
        .ok_or_else(|| {
            eyre::eyre!(
                "access pass for {} / {} disappeared between plan and apply",
                change.client_ip,
                change.user_payer
            )
        })?;

    // Address the pass that actually holds the grant. A shared pass is stored at 0.0.0.0, and
    // `set` seeds the PDA from this value — sending the concrete IP would write a different
    // account than the one the plan described.
    let signature = client.set_accesspass(SetAccessPassCommand {
        accesspass_type: pass.accesspass_type.clone(),
        client_ip: pass.client_ip,
        user_payer: pass.user_payer,
        last_access_epoch: if change.to.is_some() {
            u64::MAX
        } else {
            pass.last_access_epoch
        },
        allow_multiple_ip: pass.allow_multiple_ip(),
        tenant: change.to_pk,
        max_unicast_users: pass.max_unicast_users,
        max_multicast_users: pass.max_multicast_users,
    })?;

    Ok(signature.to_string())
}

fn emit_json<W: Write>(
    out: &mut W,
    plan: &AccessPassPlan,
    results: &[ApplyResult],
    ibrl_results: &[IbrlResult],
) -> eyre::Result<()> {
    let applied = results.iter().filter(|r| r.state == "applied").count()
        + ibrl_results.iter().filter(|r| r.state == "applied").count();
    let total = results.len() + ibrl_results.len();
    let json = serde_json::to_string_pretty(&ApplyJson {
        changed: applied > 0,
        counts: Counts {
            applied,
            failed: total - applied,
            satisfied: plan.satisfied.len(),
            blocked: plan.blocked.len(),
        },
        results,
        ibrl_results,
        plan,
    })?;
    writeln!(out, "{json}")?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::ApplyAccessPassCliCommand;
    use crate::{
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{AccountType, MulticastGroup, MulticastGroupStatus};
    use doublezero_serviceability::state::accesspass::{
        AccessPass, AccessPassStatus, AccessPassType,
    };
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::{collections::HashMap, io::Cursor, net::Ipv4Addr};

    const IP: Ipv4Addr = Ipv4Addr::new(203, 0, 113, 10);

    fn signature() -> Signature {
        Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ])
    }

    /// A client holding one group `g1` and a pass whose subscriber allowlist is `sub_allow`.
    fn fixture(
        sub_allow_has_g1: bool,
    ) -> (
        crate::doublezerocommand::MockCliCommand,
        Pubkey,
        tempfile::NamedTempFile,
    ) {
        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let group_pk = Pubkey::new_unique();

        client.expect_get_payer().returning(move || payer);
        client
            .expect_check_requirements()
            .with(mockall::predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client.expect_list_multicastgroup().returning(move |_| {
            Ok(HashMap::from([(
                group_pk,
                MulticastGroup {
                    account_type: AccountType::MulticastGroup,
                    index: 1,
                    bump_seed: 1,
                    owner: Pubkey::new_unique(),
                    tenant_pk: Pubkey::default(),
                    multicast_ip: [239, 0, 0, 1].into(),
                    max_bandwidth: 1_000_000_000,
                    status: MulticastGroupStatus::Activated,
                    code: "g1".to_string(),
                    publisher_count: 0,
                    subscriber_count: 0,
                },
            )]))
        });

        let sub_allow = if sub_allow_has_g1 {
            vec![group_pk]
        } else {
            vec![]
        };
        let pass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: IP,
            user_payer: payer,
            last_access_epoch: u64::MAX,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: sub_allow,
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), pass.clone()))));

        let doc = format!(
            "defaults:\n  user_payer: {payer}\naccess_passes:\n  - client_ip: {IP}\n    multicast:\n      subscribe: [g1]\n"
        );
        let mut file = tempfile::NamedTempFile::new().unwrap();
        std::io::Write::write_all(&mut file, doc.as_bytes()).unwrap();
        (client, payer, file)
    }

    #[test]
    fn applies_the_missing_grant_after_confirmation() {
        let (mut client, _payer, file) = fixture(false);
        client
            .expect_add_multicastgroup_sub_allowlist()
            .times(1)
            .returning(move |_| Ok(signature()));

        let mut out = Vec::new();
        let mut input = Cursor::new(b"y\n".to_vec());
        let res = block_on(
            ApplyAccessPassCliCommand {
                file: file.path().to_path_buf(),
                dry_run: false,
                auto_approve: false,
                verbose: false,
                json: false,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut out,
                &mut input,
            ),
        );

        assert!(res.is_ok(), "{res:?}");
        let text = String::from_utf8(out).unwrap();
        assert!(text.contains("+ subscriber  g1"), "{text}");
        assert!(
            text.contains("Do you want to perform these actions?"),
            "{text}"
        );
        assert!(
            text.contains("Apply complete! 1 applied, 0 failed."),
            "{text}"
        );
    }

    #[test]
    fn answering_no_sends_nothing() {
        let (mut client, _payer, file) = fixture(false);
        // No expectation set for the write: mockall fails the test if it is called at all.
        client.expect_add_multicastgroup_sub_allowlist().never();

        let mut out = Vec::new();
        let mut input = Cursor::new(b"n\n".to_vec());
        let res = block_on(
            ApplyAccessPassCliCommand {
                file: file.path().to_path_buf(),
                dry_run: false,
                auto_approve: false,
                verbose: false,
                json: false,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut out,
                &mut input,
            ),
        );

        assert!(res.is_ok(), "{res:?}");
        assert!(String::from_utf8(out).unwrap().contains("Aborted."));
    }

    #[test]
    fn dry_run_shows_the_plan_and_sends_nothing() {
        let (mut client, _payer, file) = fixture(false);
        client.expect_add_multicastgroup_sub_allowlist().never();

        let mut out = Vec::new();
        let mut input = Cursor::new(Vec::new());
        let res = block_on(
            ApplyAccessPassCliCommand {
                file: file.path().to_path_buf(),
                dry_run: true,
                auto_approve: false,
                verbose: false,
                json: false,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut out,
                &mut input,
            ),
        );

        assert!(res.is_ok(), "{res:?}");
        let text = String::from_utf8(out).unwrap();
        assert!(text.contains("+ subscriber  g1"), "{text}");
        assert!(text.contains("[dry-run] nothing was sent."), "{text}");
    }

    #[test]
    fn a_second_run_is_a_no_op_and_reports_unchanged() {
        // The pass already grants g1, which is what a re-run of a converged document looks like.
        let (mut client, _payer, file) = fixture(true);
        client.expect_add_multicastgroup_sub_allowlist().never();

        let mut out = Vec::new();
        let mut input = Cursor::new(Vec::new());
        let res = block_on(
            ApplyAccessPassCliCommand {
                file: file.path().to_path_buf(),
                dry_run: false,
                auto_approve: true,
                verbose: false,
                json: true,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut out,
                &mut input,
            ),
        );

        assert!(res.is_ok(), "{res:?}");
        let json: serde_json::Value =
            serde_json::from_slice(&out).expect("stdout must be exactly one JSON object");
        assert_eq!(json["changed"], false);
        assert_eq!(json["counts"]["applied"], 0);
        assert_eq!(json["counts"]["satisfied"], 1);
    }

    #[test]
    fn json_emits_one_object_with_changed_true_after_a_write() {
        let (mut client, _payer, file) = fixture(false);
        client
            .expect_add_multicastgroup_sub_allowlist()
            .times(1)
            .returning(move |_| Ok(signature()));

        let mut out = Vec::new();
        let mut input = Cursor::new(Vec::new());
        let res = block_on(
            ApplyAccessPassCliCommand {
                file: file.path().to_path_buf(),
                dry_run: false,
                auto_approve: true,
                verbose: false,
                json: true,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut out,
                &mut input,
            ),
        );

        assert!(res.is_ok(), "{res:?}");
        let json: serde_json::Value = serde_json::from_slice(&out).unwrap();
        assert_eq!(json["changed"], true);
        assert_eq!(json["counts"]["applied"], 1);
        assert_eq!(json["results"][0]["state"], "applied");
        assert_eq!(json["results"][0]["group"], "g1");
        assert_eq!(json["results"][0]["op"], "grant");
    }

    #[test]
    fn json_without_auto_approve_is_refused_rather_than_hanging() {
        let (client, _payer, file) = fixture(false);

        let mut out = Vec::new();
        let mut input = Cursor::new(Vec::new());
        let err = block_on(
            ApplyAccessPassCliCommand {
                file: file.path().to_path_buf(),
                dry_run: false,
                auto_approve: false,
                verbose: false,
                json: true,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut out,
                &mut input,
            ),
        )
        .unwrap_err();

        assert!(err.to_string().contains("--auto-approve"), "{err}");
    }

    /// The IBRL write must address the pass that actually holds the grant and re-send every field
    /// `access-pass set` would otherwise clobber.
    #[test]
    fn setting_the_tenant_targets_the_stored_pass_and_preserves_the_rest() {
        use doublezero_sdk::commands::accesspass::set::SetAccessPassCommand;
        use doublezero_serviceability::state::{
            accesspass::{FeedSeat, ALLOW_MULTIPLE_IP},
            tenant::{Tenant, TenantBillingConfig, TenantPaymentStatus},
        };

        let mut client = create_test_client();
        let payer = Pubkey::new_unique();
        let tenant_pk = Pubkey::new_unique();
        let feed_key = Pubkey::new_unique();

        client.expect_get_payer().returning(move || payer);
        client.expect_check_requirements().returning(|_| Ok(()));
        client
            .expect_list_multicastgroup()
            .returning(|_| Ok(HashMap::new()));
        // The pass carries a feed seat, so the planner scans feeds to work out what they grant.
        client.expect_list_feed().returning(|_| Ok(HashMap::new()));
        client.expect_list_tenant().returning(move |_| {
            Ok(HashMap::from([(
                tenant_pk,
                Tenant {
                    account_type: AccountType::Tenant,
                    owner: Pubkey::new_unique(),
                    bump_seed: 0,
                    code: "solana".to_string(),
                    vrf_id: 100,
                    reference_count: 1,
                    administrators: vec![],
                    token_account: Pubkey::default(),
                    payment_status: TenantPaymentStatus::Paid,
                    metro_routing: false,
                    route_liveness: false,
                    billing: TenantBillingConfig::default(),
                    include_topologies: vec![],
                },
            )]))
        });

        // A shared EdgeSeat pass: stored at 0.0.0.0, allow_multiple_ip set, non-default caps, a
        // finite epoch, and a feed seat. Every one of those is a field `set` would reset.
        let seats = vec![FeedSeat {
            feed_key,
            max_users: 2,
            max_future_users: 2,
            current_users: 1,
            anniversary_day: 1,
            window_end: 0,
            terminates_at: 0,
        }];
        let stored = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 255,
            accesspass_type: AccessPassType::EdgeSeat(seats.clone()),
            client_ip: Ipv4Addr::UNSPECIFIED,
            user_payer: payer,
            last_access_epoch: 200,
            connection_count: 0,
            status: AccessPassStatus::Connected,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            flags: ALLOW_MULTIPLE_IP,
            unicast_user_count: 1,
            max_unicast_users: 3,
            multicast_user_count: 2,
            max_multicast_users: 5,
        };
        client
            .expect_get_accesspass()
            .returning(move |_| Ok(Some((Pubkey::new_unique(), stored.clone()))));

        client
            .expect_set_accesspass()
            .withf(move |cmd: &SetAccessPassCommand| {
                cmd.client_ip == Ipv4Addr::UNSPECIFIED           // the stored pass, not 203.0.113.10
                    && cmd.tenant == tenant_pk
                    && cmd.last_access_epoch == u64::MAX        // a declared ibrl pins the epoch
                    && cmd.allow_multiple_ip
                    && cmd.max_unicast_users == 3
                    && cmd.max_multicast_users == 5
                    && matches!(cmd.accesspass_type, AccessPassType::EdgeSeat(_))
            })
            .times(1)
            .returning(move |_| Ok(signature()));

        let doc = format!(
            "access_passes:\n  - client_ip: {IP}\n    user_payer: {payer}\n    ibrl: solana\n"
        );
        let mut file = tempfile::NamedTempFile::new().unwrap();
        std::io::Write::write_all(&mut file, doc.as_bytes()).unwrap();

        let mut out = Vec::new();
        let mut input = Cursor::new(Vec::new());
        let res = block_on(
            ApplyAccessPassCliCommand {
                file: file.path().to_path_buf(),
                dry_run: false,
                auto_approve: true,
                verbose: false,
                json: false,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut out,
                &mut input,
            ),
        );

        assert!(res.is_ok(), "{res:?}");
        let text = String::from_utf8(out).unwrap();
        assert!(text.contains("+ ibrl        solana"), "{text}");
        assert!(
            text.contains("Apply complete! 1 applied, 0 failed."),
            "{text}"
        );
    }

    #[test]
    fn a_failed_write_is_reported_and_exits_non_zero() {
        let (mut client, _payer, file) = fixture(false);
        client
            .expect_add_multicastgroup_sub_allowlist()
            .returning(|_| Err(eyre::eyre!("NotAllowed")));

        let mut out = Vec::new();
        let mut input = Cursor::new(Vec::new());
        let err = block_on(
            ApplyAccessPassCliCommand {
                file: file.path().to_path_buf(),
                dry_run: false,
                auto_approve: true,
                verbose: false,
                json: false,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut out,
                &mut input,
            ),
        )
        .unwrap_err();

        let text = String::from_utf8(out).unwrap();
        assert!(text.contains("NotAllowed"), "{text}");
        assert!(text.contains("0 applied, 1 failed"), "{text}");
        assert!(err.to_string().contains("1 of 1"), "{err}");
    }
}
