use std::{io::Write, net::Ipv4Addr};

use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::{
    accesspass::list::ListAccessPassCommand, device::get::GetDeviceCommand,
};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{
        get_accesspass_pda, get_globalstate_pda, get_permission_pda, get_resource_extension_pda,
        get_user_pda,
    },
    processors::{
        accesspass::{close::CloseAccessPassArgs, set::SetAccessPassArgs},
        multicastgroup::{
            allowlist::subscriber::add::AddMulticastGroupSubAllowlistArgs,
            subscribe::UpdateMulticastGroupRolesArgs,
        },
        user::{create_subscribe::UserCreateSubscribeArgs, delete::UserDeleteArgs},
    },
    resource::ResourceType,
    state::{
        accesspass::AccessPass,
        user::{User, UserType},
    },
};
use solana_compute_budget_interface::ComputeBudgetInstruction;
use solana_sdk::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};
use solana_system_interface::program as system_program;

/// Re-own the shred oracle's validator-seeded access passes back to the oracle
/// (malbeclabs/infra#2031). For each pass where `owner == oracle` but
/// `user_payer != oracle`, this packs — in ONE transaction per pass — the full
/// re-own sequence: unsubscribe the Multicast user from every group, delete it,
/// close the old (validator-seeded) pass, recreate the pass oracle-seeded,
/// restore its subscriber allowlist, and recreate + re-subscribe the user
/// oracle-owned. Defaults to a dry run that builds and simulates each tx and
/// prints its serialized size; pass `--execute` to send.
#[derive(Args, Debug)]
pub struct MigrateAccessPassToOracleCliCommand {
    /// The shred oracle pubkey (the passes' current `owner`).
    #[arg(long)]
    pub oracle: Pubkey,
    /// Migrate only the pass for this client IP (default: all matching passes).
    #[arg(long)]
    pub client_ip: Option<Ipv4Addr>,
    /// Actually send the transactions. Without this flag the command only
    /// simulates and reports the transaction size (dry run).
    #[arg(long, default_value_t = false)]
    pub execute: bool,
}

/// A single migration target: an oracle-owned, validator-seeded pass and its
/// associated live Multicast user.
struct Target {
    accesspass_pk: Pubkey,
    accesspass: AccessPass,
    user_pk: Pubkey,
    user: User,
}

impl MigrateAccessPassToOracleCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        // A dry run only needs a signing identity (to assemble a realistic tx for
        // size + simulation); it spends nothing, so balance is required only for
        // an actual --execute.
        let checks = if self.execute {
            CHECK_ID_JSON | CHECK_BALANCE
        } else {
            CHECK_ID_JSON
        };
        client.check_requirements(checks)?;

        let program_id = client.get_program_id();

        // The signer becomes the recreated pass's `owner` (SetAccessPass sets
        // owner = payer) and must hold ACCESS_PASS_ADMIN + USER_ADMIN, so the
        // migration MUST be signed by the oracle key itself — not the default
        // keypair. Refuse otherwise so we can never re-own a pass to the wrong key.
        let payer = client.get_payer();
        if payer != self.oracle {
            eyre::bail!(
                "this migration must be signed by the oracle key {} (the recreated pass's owner \
                 is set to the transaction signer), but the configured signer is {}. Re-run with \
                 the oracle keypair, e.g. `-k <oracle-keypair.json>`.",
                self.oracle,
                payer,
            );
        }

        // Resolve the payer's Permission PDA once; include it in authorized
        // instructions only when it exists on-chain (mirrors the SDK's
        // execute_authorized_transaction behavior).
        let (perm_pda, _) = get_permission_pda(&program_id, &payer);
        let permission = match client.get_account(perm_pda) {
            Ok(acc) if acc.owner == program_id && !acc.data.is_empty() => Some(perm_pda),
            _ => None,
        };

        let targets = self.discover_targets(client)?;
        if targets.is_empty() {
            writeln!(out, "No matching access passes to migrate.")?;
            return Ok(());
        }

        writeln!(
            out,
            "{} target pass(es){}.\n",
            targets.len(),
            if self.execute { "" } else { " (dry run)" }
        )?;

        const LEGACY_TX_LIMIT: usize = 1232;
        let mut max_bytes = 0usize;
        let mut over_limit = 0usize;
        let mut sim_errors = 0usize;
        let mut retained = 0usize;
        let mut done = 0usize;

        for target in &targets {
            let (_, device) = client.get_device(GetDeviceCommand {
                pubkey_or_code: target.user.device_pk.to_string(),
            })?;
            let dz_prefix_count = device.dz_prefixes.len();

            if !target.user.publishers.is_empty() {
                writeln!(
                    out,
                    "SKIP {} ({}): user is a multicast publisher; not handled by this migration",
                    target.accesspass_pk, target.user.client_ip
                )?;
                continue;
            }

            // Only close the old (validator-seeded) pass when the Multicast user is its
            // sole connection. If other users still reference it (connection_count > 1 —
            // e.g. the validator's own unicast user), leave it in place for them.
            let close_old_pass = target.accesspass.connection_count <= 1;
            if !close_old_pass {
                retained += 1;
            }
            let retain_note = if close_old_pass {
                String::new()
            } else {
                format!(
                    "  [old pass retained: {} connections]",
                    target.accesspass.connection_count
                )
            };

            let ixs = build_migration_instructions(
                &program_id,
                &self.oracle,
                target,
                dz_prefix_count,
                permission,
                close_old_pass,
            )?;

            let groups = target.user.subscribers.len();
            if self.execute {
                let sig = client.send_instructions(ixs)?;
                writeln!(
                    out,
                    "MIGRATED {} ({}, {} group(s)) -> {}{}",
                    target.accesspass_pk, target.user.client_ip, groups, sig, retain_note
                )?;
                done += 1;
            } else {
                let report = client.simulate_instructions(ixs)?;
                max_bytes = max_bytes.max(report.tx_bytes);
                if report.tx_bytes > LEGACY_TX_LIMIT {
                    over_limit += 1;
                }
                if report.err.is_some() {
                    sim_errors += 1;
                }
                writeln!(
                    out,
                    "{} ({}, {} group(s), {} dz_prefix): {} ixs, {} bytes{}{}{}",
                    target.accesspass_pk,
                    target.user.client_ip,
                    groups,
                    dz_prefix_count,
                    report.instruction_count,
                    report.tx_bytes,
                    report
                        .units_consumed
                        .map(|u| format!(", {u} CU"))
                        .unwrap_or_default(),
                    retain_note,
                    match &report.err {
                        Some(e) => format!("  SIM ERROR: {e}"),
                        None => String::new(),
                    }
                )?;
                if report.err.is_some() {
                    for line in &report.logs {
                        writeln!(out, "    {line}")?;
                    }
                }
                done += 1;
            }
        }

        if self.execute {
            writeln!(
                out,
                "\nMigrated {done} pass(es); {retained} kept their old pass (shared with another user)."
            )?;
        } else {
            writeln!(
                out,
                "\nSimulated {done} pass(es): max {max_bytes} bytes (limit {LEGACY_TX_LIMIT}), \
                 {over_limit} over limit, {sim_errors} sim error(s), {retained} with old pass retained."
            )?;
        }

        Ok(())
    }

    /// Passes where `owner == oracle` and `user_payer != oracle`, each with a
    /// live Multicast user at the pass's client IP (`User.owner == user_payer`).
    fn discover_targets<C: CliCommand>(&self, client: &C) -> eyre::Result<Vec<Target>> {
        let program_id = client.get_program_id();
        let passes = client.list_accesspass(ListAccessPassCommand)?;

        let mut targets = Vec::new();
        for (accesspass_pk, accesspass) in passes {
            if accesspass.owner != self.oracle || accesspass.user_payer == self.oracle {
                continue;
            }
            if let Some(ip) = self.client_ip {
                if accesspass.client_ip != ip {
                    continue;
                }
            }

            let (user_pk, _) =
                get_user_pda(&program_id, &accesspass.client_ip, UserType::Multicast);
            let user = match client
                .get_account_data(user_pk)
                .ok()
                .and_then(|d| d.get_user().ok())
            {
                Some(user)
                    if user.owner == accesspass.user_payer
                        && user.client_ip == accesspass.client_ip =>
                {
                    user
                }
                // No live Multicast user at this IP for this validator: orphan
                // pass or a probe-first-reuse case (infra#2032). Skip here.
                _ => continue,
            };

            targets.push(Target {
                accesspass_pk,
                accesspass,
                user_pk,
                user,
            });
        }
        Ok(targets)
    }
}

/// Trailing accounts appended to an `authorize()`-gated instruction:
/// `[payer(signer), system, permission?]`.
fn authorized_trailing(payer: &Pubkey, permission: Option<Pubkey>) -> Vec<AccountMeta> {
    let mut metas = vec![
        AccountMeta::new(*payer, true),
        AccountMeta::new_readonly(system_program::ID, false),
    ];
    if let Some(perm) = permission {
        metas.push(AccountMeta::new_readonly(perm, false));
    }
    metas
}

fn ix(
    program_id: &Pubkey,
    instruction: DoubleZeroInstruction,
    mut accounts: Vec<AccountMeta>,
    trailing: Vec<AccountMeta>,
) -> Instruction {
    accounts.extend(trailing);
    Instruction::new_with_bytes(*program_id, &instruction.pack(), accounts)
}

/// Build the full per-pass re-own transaction (see the command doc comment for
/// the sequence). `oracle` is the target owner; `dz_prefix_count` is the
/// device's `dz_prefixes.len()`.
fn build_migration_instructions(
    program_id: &Pubkey,
    oracle: &Pubkey,
    target: &Target,
    dz_prefix_count: usize,
    permission: Option<Pubkey>,
    close_old_pass: bool,
) -> eyre::Result<Vec<Instruction>> {
    let client_ip = target.user.client_ip;
    let device_pk = target.user.device_pk;
    let payer = *oracle; // the migration runs signed by the oracle/admin

    let (globalstate, _) = get_globalstate_pda(program_id);
    let ap_old = target.accesspass_pk;
    let (ap_new, _) = get_accesspass_pda(program_id, &client_ip, oracle);
    let user_pk = target.user_pk;

    let (utb, _, _) = get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
    let (mpb, _, _) = get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
    let (tid, _, _) = get_resource_extension_pda(program_id, ResourceType::TunnelIds(device_pk, 0));
    let dz_prefixes: Vec<Pubkey> = (0..dz_prefix_count)
        .map(|i| {
            get_resource_extension_pda(program_id, ResourceType::DzPrefixBlock(device_pk, i)).0
        })
        .collect();
    let dz_prefix_count_u8 = u8::try_from(dz_prefix_count)
        .map_err(|_| eyre::eyre!("device has too many dz_prefixes ({dz_prefix_count})"))?;

    let groups = target.user.subscribers.clone();
    let mut ixs = Vec::new();

    // Realistic compute + heap headroom, matching the SDK's single-ix envelope.
    ixs.push(ComputeBudgetInstruction::set_compute_unit_limit(1_400_000));
    ixs.push(ComputeBudgetInstruction::request_heap_frame(256 * 1024));

    // 1) Unsubscribe the (still validator-owned) user from every group so
    //    DeleteUser's "no active subscriptions" guard passes.
    for group in &groups {
        ixs.push(ix(
            program_id,
            DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
                publisher: false,
                subscriber: false,
                client_ip,
                use_onchain_allocation: true,
            }),
            vec![
                AccountMeta::new(*group, false),
                AccountMeta::new(ap_old, false),
                AccountMeta::new(user_pk, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(mpb, false),
            ],
            authorized_trailing(&payer, permission),
        ));
    }

    // 2) Delete the user (frees tunnel_id / tunnel_net, decrements the old
    //    pass's connection_count to 0).
    let mut delete_accounts = vec![
        AccountMeta::new(user_pk, false),
        AccountMeta::new(ap_old, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(device_pk, false),
        AccountMeta::new(utb, false),
        AccountMeta::new(mpb, false),
        AccountMeta::new(tid, false),
    ];
    for dz in &dz_prefixes {
        delete_accounts.push(AccountMeta::new(*dz, false));
    }
    if target.user.tenant_pk != Pubkey::default() {
        delete_accounts.push(AccountMeta::new(target.user.tenant_pk, false));
    }
    delete_accounts.push(AccountMeta::new(target.user.owner, false));
    ixs.push(ix(
        program_id,
        DoubleZeroInstruction::DeleteUser(UserDeleteArgs {
            dz_prefix_count: dz_prefix_count_u8,
            multicast_publisher_count: 1,
        }),
        delete_accounts,
        authorized_trailing(&payer, permission),
    ));

    // 3) Close the old (validator-seeded) pass — only when the Multicast user we just
    //    deleted was its sole connection (connection_count is now 0). If other users
    //    still reference it, the caller sets close_old_pass = false and it stays.
    if close_old_pass {
        ixs.push(ix(
            program_id,
            DoubleZeroInstruction::CloseAccessPass(CloseAccessPassArgs {}),
            vec![
                AccountMeta::new(ap_old, false),
                AccountMeta::new(globalstate, false),
            ],
            authorized_trailing(&payer, permission),
        ));
    }

    // 4) Recreate the pass oracle-seeded (owner = payer = oracle, user_payer =
    //    oracle). Every non-allowlist field is preserved from the old pass.
    ixs.push(ix(
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: target.accesspass.accesspass_type.clone(),
            client_ip,
            last_access_epoch: target.accesspass.last_access_epoch,
            allow_multiple_ip: target.accesspass.allow_multiple_ip(),
            max_unicast_users: target.accesspass.max_unicast_users,
            max_multicast_users: target.accesspass.max_multicast_users,
        }),
        vec![
            AccountMeta::new(ap_new, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(*oracle, false), // user_payer
        ],
        authorized_trailing(&payer, permission),
    ));

    // 5) Restore the subscriber allowlist on the new pass (SetAccessPass creates
    //    it with empty allowlists; subscribe checks the allowlist per group).
    for group in &groups {
        ixs.push(ix(
            program_id,
            DoubleZeroInstruction::AddMulticastGroupSubAllowlist(
                AddMulticastGroupSubAllowlistArgs {
                    client_ip,
                    user_payer: *oracle,
                },
            ),
            vec![
                AccountMeta::new(*group, false),
                AccountMeta::new(ap_new, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(*oracle, false), // user_payer
            ],
            authorized_trailing(&payer, permission),
        ));
    }

    // 6) Recreate the user oracle-owned and subscribe it to the first group
    //    (this re-allocates tunnel_id / tunnel_net).
    let (first_group, rest_groups) = groups
        .split_first()
        .ok_or_else(|| eyre::eyre!("user has no subscriber groups"))?;
    let mut create_accounts = vec![
        AccountMeta::new(user_pk, false),
        AccountMeta::new(device_pk, false),
        AccountMeta::new(*first_group, false),
        AccountMeta::new(ap_new, false),
        AccountMeta::new(globalstate, false),
        AccountMeta::new(utb, false),
        AccountMeta::new(mpb, false),
        AccountMeta::new(tid, false),
    ];
    for dz in &dz_prefixes {
        create_accounts.push(AccountMeta::new(*dz, false));
    }
    ixs.push(ix(
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: UserType::Multicast,
            cyoa_type: target.user.cyoa_type,
            client_ip,
            publisher: false,
            subscriber: true,
            tunnel_endpoint: target.user.tunnel_endpoint,
            dz_prefix_count: dz_prefix_count_u8,
            owner: *oracle,
        }),
        create_accounts,
        authorized_trailing(&payer, permission),
    ));

    // 7) Re-subscribe the (now oracle-owned) user to the remaining groups.
    for group in rest_groups {
        ixs.push(ix(
            program_id,
            DoubleZeroInstruction::UpdateMulticastGroupRoles(UpdateMulticastGroupRolesArgs {
                publisher: false,
                subscriber: true,
                client_ip,
                use_onchain_allocation: true,
            }),
            vec![
                AccountMeta::new(*group, false),
                AccountMeta::new(ap_new, false),
                AccountMeta::new(user_pk, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(mpb, false),
            ],
            authorized_trailing(&payer, permission),
        ));
    }

    Ok(ixs)
}
