//! Hidden `user recreate` verb: delete and recreate a `User` account in one atomic
//! transaction, verified by simulation before it ever sends.

use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey,
};
use clap::Args;
use doublezero_cli_core::{print_signature, render_collection, CliContext, OutputFormat};
use doublezero_config::Environment;
use doublezero_sdk::{
    commands::user::{get::GetUserCommand, recreate::RecreateUserCommand},
    User, UserStatus,
};
use doublezero_serviceability::{pda::get_user_pda, state::user::TunnelFlags};
use serde::Serialize;
use solana_sdk::{pubkey::Pubkey, transaction::Transaction};
use std::{io::Write, str::FromStr};
use tabled::Tabled;

/// Solana's max serialized transaction size (`solana_packet::PACKET_DATA_SIZE`).
const MAX_TRANSACTION_BYTES: usize = 1232;

#[derive(Args, Debug)]
pub struct RecreateUserCliCommand {
    /// User Pubkey to delete and recreate
    #[arg(long, value_parser = validate_pubkey)]
    pub pubkey: String,
    /// Simulate and report the predicted diff without sending
    #[arg(long)]
    pub dry_run: bool,
}

impl RecreateUserCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        // Mainnet guard: first statement, before any RPC.
        if ctx.env == Environment::MainnetBeta {
            eyre::bail!("user recreate is not permitted on mainnet-beta");
        }
        tracing::debug!(env = %ctx.env, pubkey = %self.pubkey, dry_run = self.dry_run, "user recreate");

        // Mainnet guard, continued: `--url`/`--program-id` can move the resolved
        // program ID to mainnet-beta independently of `--env`, so the check above is
        // bypassable by flag combination. Catch it via the resolved program ID too,
        // before anything is planned, simulated, or sent.
        let program_id = client.get_program_id();
        if matches!(
            Environment::from_program_id(&program_id.to_string()),
            Ok(Environment::MainnetBeta)
        ) {
            eyre::bail!("user recreate is not permitted on mainnet-beta");
        }

        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey = Pubkey::from_str(&self.pubkey)?;
        let plan = client.plan_recreate_user(RecreateUserCommand { pubkey })?;
        let user_before = &plan.user_before;

        // Precondition 2: `plan()` returns `user_pk` as the caller-supplied pubkey, while
        // `create_user` derives its target address from (client_ip, user_type). A legacy
        // index-derived user whose supplied pubkey does not match that PDA would otherwise
        // either fail loudly (the multicast case) or silently relocate to a different
        // address (the plain two-instruction case). Refuse both up front.
        let (expected_pda, _) =
            get_user_pda(&program_id, &user_before.client_ip, user_before.user_type);
        if plan.user_pk != expected_pda {
            eyre::bail!(
                "user {} does not resolve to the current (client_ip, user_type) PDA (expected \
                 {expected_pda}); it appears to live at a legacy index-derived PDA and cannot be \
                 safely recreated at the same address",
                plan.user_pk
            );
        }

        // Precondition 4: EdgeSeat feed seats are released by delete and cannot be
        // restored by resubscription.
        if !user_before.feed_pks.is_empty() {
            eyre::bail!(
                "user {} holds {} EdgeSeat feed seat(s); these are released by delete and cannot \
                 be restored by resubscription",
                plan.user_pk,
                user_before.feed_pks.len()
            );
        }

        // Precondition 5: usability guard, not enforcement. The program permits a
        // USER_ADMIN key to delete another owner's user and create one owned by the payer
        // (see processors/user/delete.rs), so this cannot prevent a determined caller.
        // It only warns an ordinary operator before it happens by accident.
        let payer = client.get_payer();
        if user_before.owner != payer {
            eyre::bail!(
                "user {} is owned by {}, not the caller {payer}; recreating would transfer \
                 ownership to the caller because UserCreate always assigns the payer as owner",
                plan.user_pk,
                user_before.owner
            );
        }

        // Precondition 6: UserCreate always creates with the publisher flag off, so a user
        // originally activated as a multicast publisher cannot have it restored, and the
        // device's multicast publisher counters would skew.
        if TunnelFlags::is_set(user_before.tunnel_flags, TunnelFlags::CreatedAsPublisher) {
            eyre::bail!(
                "user {} was created as a multicast publisher (tunnel_flags CreatedAsPublisher); \
                 UserCreate always clears this flag, so recreating would desync the device's \
                 multicast publisher counters",
                plan.user_pk
            );
        }

        // Precondition 3: the assembled transaction must still fit in a single packet.
        let tx = Transaction::new_with_payer(&plan.instructions, Some(&payer));
        let size = bincode::serde::encode_to_vec(&tx, bincode::config::legacy())?.len();
        if size > MAX_TRANSACTION_BYTES {
            let group_count = user_before.get_multicast_groups().len();
            eyre::bail!(
                "recreate transaction would be {size} bytes, exceeding the \
                 {MAX_TRANSACTION_BYTES}-byte limit, due to {group_count} multicast group \
                 membership(s); pick a user with fewer multicast groups"
            );
        }

        let outcome = client.simulate_recreate_user(RecreateUserCommand { pubkey }, &plan)?;
        if let Some(err) = &outcome.err {
            eyre::bail!(
                "simulation failed: {err}\nlogs:\n{}",
                outcome.logs.join("\n")
            );
        }
        match outcome.units_consumed {
            Some(units) => writeln!(out, "Units consumed: {units}")?,
            None => writeln!(out, "Units consumed: unknown")?,
        }

        let account_data = outcome
            .accounts
            .first()
            .and_then(Option::as_ref)
            .map(Vec::as_slice)
            .ok_or_else(|| {
                eyre::eyre!(
                    "simulation returned no post-simulation state for user {}",
                    plan.user_pk
                )
            })?;
        let simulated_user = User::try_from(account_data)
            .map_err(|e| eyre::eyre!("failed to decode simulated user account: {e:?}"))?;
        let simulated_changes = classify(user_before, &simulated_user);
        print_fidelity_table(out, "Simulated fidelity:", &simulated_changes)?;

        if let Some(unexpected) = unexpected_fields(&simulated_changes) {
            eyre::bail!(
                "simulated recreate would not preserve field(s) {unexpected}; refusing to send"
            );
        }

        if self.dry_run {
            return Ok(());
        }

        let signature = client.recreate_user(RecreateUserCommand { pubkey }, &plan)?;
        print_signature(out, &signature)?;

        // Simulation runs against a recent bank, not the executing one. Re-check against
        // the confirmed state before declaring success.
        let (_, confirmed_user) = client.get_user(GetUserCommand { pubkey })?;
        let confirmed_changes = classify(user_before, &confirmed_user);
        print_fidelity_table(out, "Confirmed fidelity:", &confirmed_changes)?;

        if let Some(unexpected) = unexpected_fields(&confirmed_changes) {
            eyre::bail!(
                "recreate sent (signature {signature}) but confirmed state differs unexpectedly \
                 in field(s) {unexpected}; investigate before retrying"
            );
        }

        Ok(())
    }
}

/// Field-level classification of a recreate round trip.
#[derive(Debug, PartialEq)]
enum FieldChange {
    Preserved,
    ReallocatedSame,
    ReallocatedChanged { before: String, after: String },
    ResetExpected { before: String, after: String },
    Unexpected { before: String, after: String },
}

/// Classify every User field across a recreate. `Unexpected` anywhere means the round
/// trip did not preserve what it promised, and the caller must not proceed.
///
/// - `bgp_status`, `last_bgp_up_at`, `last_bgp_reported_at`, `bgp_rtt_ns`: telemetry the
///   delete+create cycle implicitly clears, so a diff is expected.
/// - `status`: expected to move to `Activated` if it was not already. Any other change
///   is unexpected.
/// - `dz_ip`, `tunnel_id`, `tunnel_net`: freshly (re)allocated by `create_user`, so either
///   landing on the same value or a different one is fine.
/// - everything else: must round-trip exactly.
fn classify(before: &User, after: &User) -> Vec<(&'static str, FieldChange)> {
    // Exhaustive destructure: adding a field to `User` must break this build rather
    // than silently escape classification, because an unclassified field would be
    // reported as a clean round trip.
    let User {
        account_type: _,
        owner: _,
        index: _,
        bump_seed: _,
        user_type: _,
        tenant_pk: _,
        device_pk: _,
        cyoa_type: _,
        client_ip: _,
        dz_ip: _,
        tunnel_id: _,
        tunnel_net: _,
        status: _,
        publishers: _,
        subscribers: _,
        validator_pubkey: _,
        tunnel_endpoint: _,
        tunnel_flags: _,
        bgp_status: _,
        last_bgp_up_at: _,
        last_bgp_reported_at: _,
        bgp_rtt_ns: _,
        feed_pks: _,
    } = before;
    vec![
        reset_expected_field("bgp_status", &before.bgp_status, &after.bgp_status),
        reset_expected_field(
            "last_bgp_up_at",
            &before.last_bgp_up_at,
            &after.last_bgp_up_at,
        ),
        reset_expected_field(
            "last_bgp_reported_at",
            &before.last_bgp_reported_at,
            &after.last_bgp_reported_at,
        ),
        reset_expected_field("bgp_rtt_ns", &before.bgp_rtt_ns, &after.bgp_rtt_ns),
        classify_status(before.status, after.status),
        reallocated_field("dz_ip", &before.dz_ip, &after.dz_ip),
        reallocated_field("tunnel_id", &before.tunnel_id, &after.tunnel_id),
        reallocated_field("tunnel_net", &before.tunnel_net, &after.tunnel_net),
        preserved_field("account_type", &before.account_type, &after.account_type),
        preserved_field("owner", &before.owner, &after.owner),
        preserved_field("index", &before.index, &after.index),
        preserved_field("bump_seed", &before.bump_seed, &after.bump_seed),
        preserved_field("user_type", &before.user_type, &after.user_type),
        preserved_field("tenant_pk", &before.tenant_pk, &after.tenant_pk),
        preserved_field("device_pk", &before.device_pk, &after.device_pk),
        preserved_field("cyoa_type", &before.cyoa_type, &after.cyoa_type),
        preserved_field("client_ip", &before.client_ip, &after.client_ip),
        preserved_field("publishers", &before.publishers, &after.publishers),
        preserved_field("subscribers", &before.subscribers, &after.subscribers),
        preserved_field(
            "validator_pubkey",
            &before.validator_pubkey,
            &after.validator_pubkey,
        ),
        preserved_field(
            "tunnel_endpoint",
            &before.tunnel_endpoint,
            &after.tunnel_endpoint,
        ),
        preserved_field("tunnel_flags", &before.tunnel_flags, &after.tunnel_flags),
        preserved_field("feed_pks", &before.feed_pks, &after.feed_pks),
    ]
}

fn reset_expected_field<T: std::fmt::Debug + PartialEq>(
    name: &'static str,
    before: &T,
    after: &T,
) -> (&'static str, FieldChange) {
    let change = if before == after {
        FieldChange::Preserved
    } else {
        FieldChange::ResetExpected {
            before: format!("{before:?}"),
            after: format!("{after:?}"),
        }
    };
    (name, change)
}

fn reallocated_field<T: std::fmt::Debug + PartialEq>(
    name: &'static str,
    before: &T,
    after: &T,
) -> (&'static str, FieldChange) {
    let change = if before == after {
        FieldChange::ReallocatedSame
    } else {
        FieldChange::ReallocatedChanged {
            before: format!("{before:?}"),
            after: format!("{after:?}"),
        }
    };
    (name, change)
}

fn preserved_field<T: std::fmt::Debug + PartialEq>(
    name: &'static str,
    before: &T,
    after: &T,
) -> (&'static str, FieldChange) {
    let change = if before == after {
        FieldChange::Preserved
    } else {
        FieldChange::Unexpected {
            before: format!("{before:?}"),
            after: format!("{after:?}"),
        }
    };
    (name, change)
}

/// `status` is special-cased: recovering to `Activated` from anything else is the one
/// expected transition (`User::try_activate` always sets `Activated`). Any other change
/// (or a change away from `Activated`) is unexpected.
fn classify_status(before: UserStatus, after: UserStatus) -> (&'static str, FieldChange) {
    let change = if before == after {
        FieldChange::Preserved
    } else if before != UserStatus::Activated && after == UserStatus::Activated {
        FieldChange::ResetExpected {
            before: format!("{before:?}"),
            after: format!("{after:?}"),
        }
    } else {
        FieldChange::Unexpected {
            before: format!("{before:?}"),
            after: format!("{after:?}"),
        }
    };
    ("status", change)
}

/// Names of every field that classified as `Unexpected`, joined for an error message.
fn unexpected_fields(changes: &[(&'static str, FieldChange)]) -> Option<String> {
    let names = changes
        .iter()
        .filter_map(|(name, change)| {
            matches!(change, FieldChange::Unexpected { .. }).then_some(*name)
        })
        .collect::<Vec<&str>>();
    (!names.is_empty()).then(|| names.join(", "))
}

#[derive(Tabled, Serialize)]
struct FidelityRow {
    field: String,
    change: String,
    before: String,
    after: String,
}

fn print_fidelity_table<W: Write>(
    out: &mut W,
    title: &str,
    changes: &[(&'static str, FieldChange)],
) -> eyre::Result<()> {
    writeln!(out, "{title}")?;
    let rows = changes
        .iter()
        .map(|(field, change)| {
            let (label, before, after) = match change {
                FieldChange::Preserved => ("preserved", String::new(), String::new()),
                FieldChange::ReallocatedSame => {
                    ("reallocated (same)", String::new(), String::new())
                }
                FieldChange::ReallocatedChanged { before, after } => {
                    ("reallocated (changed)", before.clone(), after.clone())
                }
                FieldChange::ResetExpected { before, after } => {
                    ("reset (expected)", before.clone(), after.clone())
                }
                FieldChange::Unexpected { before, after } => {
                    ("UNEXPECTED", before.clone(), after.clone())
                }
            };
            FidelityRow {
                field: field.to_string(),
                change: label.to_string(),
                before,
                after,
            }
        })
        .collect();
    render_collection(out, rows, OutputFormat::Table)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_cli_core::testing::{
        block_on, cli_context_default_for_tests, cli_context_for_tests,
    };
    use doublezero_sdk::{
        commands::user::recreate::RecreatePlan, doublezeroclient::SimulationOutcome, AccountType,
        BGPStatus, UserCYOA, UserType,
    };
    use doublezero_serviceability::pda::get_user_old_pda;
    use mockall::predicate;
    use solana_sdk::{instruction::Instruction, signature::Signature};
    use std::net::Ipv4Addr;

    fn base_user(owner: Pubkey) -> User {
        User {
            account_type: AccountType::User,
            owner,
            index: 0,
            bump_seed: 1,
            user_type: UserType::IBRL,
            tenant_pk: Pubkey::default(),
            device_pk: Pubkey::new_unique(),
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: [198, 51, 100, 7].into(),
            dz_ip: [198, 51, 100, 20].into(),
            tunnel_id: 500,
            tunnel_net: "169.254.0.0/31".parse().unwrap(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: BGPStatus::Up,
            last_bgp_up_at: 100,
            last_bgp_reported_at: 100,
            bgp_rtt_ns: 5_000_000,
            feed_pks: vec![],
        }
    }

    fn dummy_instructions() -> Vec<Instruction> {
        vec![
            Instruction::new_with_bytes(Pubkey::new_unique(), &[0], vec![]),
            Instruction::new_with_bytes(Pubkey::new_unique(), &[1], vec![]),
        ]
    }

    /// Builds a mock client stubbed for a successful `check_requirements` + `plan`, with
    /// `user_before` addressed at its correct (client_ip, user_type) PDA, and returns the
    /// client, that PDA, and the plan for further per-test customization.
    fn setup(
        build_user: impl FnOnce(Pubkey) -> User,
        instructions: Vec<Instruction>,
    ) -> (
        crate::doublezerocommand::MockCliCommand,
        Pubkey,
        RecreatePlan,
    ) {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let user = build_user(payer);
        let program_id = client.get_program_id();
        let (user_pk, _) = get_user_pda(&program_id, &user.client_ip, user.user_type);
        let plan = RecreatePlan {
            instructions,
            user_pk,
            user_before: user,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        let plan_for_mock = plan.clone();
        client
            .expect_plan_recreate_user()
            .with(predicate::eq(RecreateUserCommand { pubkey: user_pk }))
            .returning(move |_| Ok(plan_for_mock.clone()));

        (client, user_pk, plan)
    }

    // 1. Mainnet refusal: the guard must run before any I/O, including `plan`.
    #[test]
    fn recreate_refuses_on_mainnet_before_any_io() {
        let mut client = create_test_client();
        client.expect_check_requirements().times(0);
        client.expect_plan_recreate_user().times(0);
        client.expect_recreate_user().times(0);

        let ctx = cli_context_for_tests()
            .with_env(Environment::MainnetBeta)
            .build()
            .unwrap();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: Pubkey::new_unique().to_string(),
                dry_run: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("mainnet"),
            "error should mention mainnet: {err}"
        );
    }

    // Mainnet refusal via resolved program ID: `--url`/`--program-id` can move the
    // resolved program ID to mainnet-beta independently of `--env`, so the `ctx.env`
    // check alone is bypassable. The guard must catch that too, before any I/O.
    #[test]
    fn recreate_refuses_when_program_id_resolves_to_mainnet() {
        let mainnet_program_id = Environment::MainnetBeta
            .config()
            .unwrap()
            .serviceability_program_id;

        let mut client = crate::doublezerocommand::MockCliCommand::new();
        client
            .expect_get_program_id()
            .returning(move || mainnet_program_id);
        client.expect_check_requirements().times(0);
        client.expect_plan_recreate_user().times(0);
        client.expect_recreate_user().times(0);

        let ctx = cli_context_for_tests()
            .with_env(Environment::Testnet)
            .with_serviceability_program_id(mainnet_program_id)
            .build()
            .unwrap();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: Pubkey::new_unique().to_string(),
                dry_run: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("mainnet"),
            "error should mention mainnet: {err}"
        );
    }

    // 2. Legacy-PDA refusal: the supplied pubkey does not match the (client_ip, user_type)
    // PDA the create side of the plan actually targets.
    #[test]
    fn recreate_refuses_legacy_index_derived_pda() {
        let mut client = create_test_client();
        let payer = client.get_payer();
        let program_id = client.get_program_id();
        let user = base_user(payer);
        let (legacy_pk, _) = get_user_old_pda(&program_id, 1);
        let plan = RecreatePlan {
            instructions: dummy_instructions(),
            user_pk: legacy_pk,
            user_before: user,
        };

        client.expect_check_requirements().returning(|_| Ok(()));
        let plan_for_mock = plan.clone();
        client
            .expect_plan_recreate_user()
            .returning(move |_| Ok(plan_for_mock.clone()));
        client.expect_simulate_recreate_user().times(0);
        client.expect_recreate_user().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: legacy_pk.to_string(),
                dry_run: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("legacy"),
            "error should mention the legacy index-derived PDA: {err}"
        );
    }

    // 3. EdgeSeat refusal: feed seats are released by delete and cannot come back.
    #[test]
    fn recreate_refuses_user_holding_feed_seats() {
        let (mut client, user_pk, _plan) = setup(
            |payer| {
                let mut user = base_user(payer);
                user.feed_pks = vec![Pubkey::new_unique()];
                user
            },
            dummy_instructions(),
        );
        client.expect_simulate_recreate_user().times(0);
        client.expect_recreate_user().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: user_pk.to_string(),
                dry_run: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("feed"),
            "error should mention feed seats: {err}"
        );
    }

    // 4. Not-owner refusal: recreating would silently transfer the user to the caller.
    #[test]
    fn recreate_refuses_when_caller_is_not_owner() {
        let (mut client, user_pk, plan) = setup(
            |_payer| base_user(Pubkey::new_unique()),
            dummy_instructions(),
        );
        client.expect_simulate_recreate_user().times(0);
        client.expect_recreate_user().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: user_pk.to_string(),
                dry_run: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        let msg = err.to_string();
        assert!(
            msg.contains(&plan.user_before.owner.to_string()),
            "error should name the current owner: {msg}"
        );
        assert!(
            msg.contains(&client.get_payer().to_string()),
            "error should name the caller: {msg}"
        );
        assert!(
            msg.contains("transfer"),
            "error should explain the transfer consequence: {msg}"
        );
    }

    // 5. Created-as-publisher refusal: UserCreate always clears the flag.
    #[test]
    fn recreate_refuses_user_created_as_publisher() {
        let (mut client, user_pk, _plan) = setup(
            |payer| {
                let mut user = base_user(payer);
                user.tunnel_flags = TunnelFlags::CreatedAsPublisher as u8;
                user
            },
            dummy_instructions(),
        );
        client.expect_simulate_recreate_user().times(0);
        client.expect_recreate_user().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: user_pk.to_string(),
                dry_run: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("publisher"),
            "error should explain the publisher flag cannot be restored: {err}"
        );
    }

    // 6. Oversize refusal: the assembled transaction would exceed a single packet.
    #[test]
    fn recreate_refuses_oversize_transaction() {
        let group_a = Pubkey::new_unique();
        let group_b = Pubkey::new_unique();
        let group_c = Pubkey::new_unique();
        let oversized_instructions = vec![Instruction::new_with_bytes(
            Pubkey::new_unique(),
            &[0u8; 2_000], // larger alone than the 1232-byte transaction limit
            vec![],
        )];
        let (mut client, user_pk, _plan) = setup(
            |payer| {
                let mut user = base_user(payer);
                user.subscribers = vec![group_a, group_b, group_c];
                user
            },
            oversized_instructions,
        );
        client.expect_simulate_recreate_user().times(0);
        client.expect_recreate_user().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: user_pk.to_string(),
                dry_run: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        let msg = err.to_string();
        assert!(msg.contains("1232"), "error should name the limit: {msg}");
        assert!(
            msg.contains("3 multicast group"),
            "error should name the multicast group count: {msg}"
        );
    }

    // 7. Dry-run: only expected-reset diffs simulate cleanly, and nothing is sent.
    #[test]
    fn recreate_dry_run_reports_fidelity_without_sending() {
        let (mut client, user_pk, plan) = setup(base_user, dummy_instructions());

        let mut simulated = plan.user_before;
        simulated.bgp_status = BGPStatus::Unknown;
        simulated.last_bgp_up_at = 0;
        simulated.last_bgp_reported_at = 0;
        simulated.bgp_rtt_ns = 0;
        simulated.dz_ip = [198, 51, 100, 99].into();
        let outcome = SimulationOutcome {
            units_consumed: Some(1_234),
            err: None,
            logs: vec![],
            accounts: vec![Some(borsh::to_vec(&simulated).unwrap())],
        };
        client
            .expect_simulate_recreate_user()
            .returning(move |_, _| Ok(outcome.clone()));
        client.expect_recreate_user().times(0);
        client.expect_get_user().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: user_pk.to_string(),
                dry_run: true,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(
            res.is_ok(),
            "dry run with only expected diffs should succeed: {res:?}"
        );
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("bgp_rtt_ns"),
            "fidelity table should list bgp_rtt_ns: {output_str}"
        );
        assert!(
            output_str.contains("dz_ip"),
            "fidelity table should list dz_ip: {output_str}"
        );
        assert!(
            !output_str.contains("Signature:"),
            "dry run must not print a signature: {output_str}"
        );
    }

    // 8. Unexpected simulated diff aborts before sending: the assertion the verb exists for.
    #[test]
    fn recreate_aborts_before_sending_on_unexpected_simulated_diff() {
        let (mut client, user_pk, plan) = setup(base_user, dummy_instructions());

        let mut simulated = plan.user_before;
        simulated.device_pk = Pubkey::new_unique();
        let outcome = SimulationOutcome {
            units_consumed: Some(999),
            err: None,
            logs: vec![],
            accounts: vec![Some(borsh::to_vec(&simulated).unwrap())],
        };
        client
            .expect_simulate_recreate_user()
            .returning(move |_, _| Ok(outcome.clone()));
        client.expect_recreate_user().times(0);
        client.expect_get_user().times(0);

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: user_pk.to_string(),
                dry_run: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("device_pk"),
            "error should name the unexpectedly-changed field: {err}"
        );
    }

    // 9. Full success path: simulated diffs are all expected, the recreate is sent, and
    // the confirmed re-fetch is reclassified too. Not one of the brief's 8 named tests,
    // but the send-and-reconfirm branch (step 13-14) is otherwise entirely untested.
    #[test]
    fn recreate_sends_and_reconfirms_on_success() {
        let (mut client, user_pk, plan) = setup(base_user, dummy_instructions());

        let mut simulated = plan.user_before;
        simulated.bgp_status = BGPStatus::Unknown;
        simulated.last_bgp_up_at = 0;
        simulated.last_bgp_reported_at = 0;
        simulated.bgp_rtt_ns = 0;
        let outcome = SimulationOutcome {
            units_consumed: Some(500),
            err: None,
            logs: vec![],
            accounts: vec![Some(borsh::to_vec(&simulated).unwrap())],
        };
        client
            .expect_simulate_recreate_user()
            .returning(move |_, _| Ok(outcome.clone()));

        let signature = Signature::from([7u8; 64]);
        client
            .expect_recreate_user()
            .returning(move |_, _| Ok(signature));

        let confirmed = simulated;
        client
            .expect_get_user()
            .with(predicate::eq(GetUserCommand { pubkey: user_pk }))
            .returning(move |_| Ok((user_pk, confirmed.clone())));

        let ctx = cli_context_default_for_tests();
        let mut output = Vec::new();
        let res = block_on(
            RecreateUserCliCommand {
                pubkey: user_pk.to_string(),
                dry_run: false,
            }
            .execute(&ctx, &client, &mut output),
        );
        assert!(res.is_ok(), "full recreate should succeed: {res:?}");
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains(&format!("Signature: {signature}")),
            "output should print the signature: {output_str}"
        );
        assert!(
            output_str.contains("Confirmed fidelity"),
            "output should print the confirmed fidelity table: {output_str}"
        );
    }

    // classify: identical users -> every field either Preserved or ReallocatedSame (the
    // three reallocated fields do not collapse into Preserved even when equal).
    #[test]
    fn classify_identical_users_has_no_diffs() {
        let user = base_user(Pubkey::new_unique());
        let changes = classify(&user, &user);
        for (field, change) in &changes {
            assert!(
                matches!(
                    change,
                    FieldChange::Preserved | FieldChange::ReallocatedSame
                ),
                "field {field} unexpectedly classified as {change:?} for identical users"
            );
        }
        let dz_ip = changes.iter().find(|(name, _)| *name == "dz_ip").unwrap();
        assert_eq!(dz_ip.1, FieldChange::ReallocatedSame);
    }

    // classify: only bgp_rtt_ns differs -> ResetExpected, and nothing is Unexpected.
    #[test]
    fn classify_bgp_rtt_diff_is_reset_expected() {
        let before = base_user(Pubkey::new_unique());
        let mut after = before.clone();
        after.bgp_rtt_ns = 42;
        let changes = classify(&before, &after);

        let bgp_rtt = changes
            .iter()
            .find(|(name, _)| *name == "bgp_rtt_ns")
            .unwrap();
        assert_eq!(
            bgp_rtt.1,
            FieldChange::ResetExpected {
                before: before.bgp_rtt_ns.to_string(),
                after: "42".to_string(),
            }
        );
        assert!(
            !changes
                .iter()
                .any(|(_, c)| matches!(c, FieldChange::Unexpected { .. })),
            "no field should be Unexpected: {changes:?}"
        );
    }

    // classify: status recovering OutOfCredits -> Activated is ResetExpected.
    #[test]
    fn classify_status_recovery_to_activated_is_reset_expected() {
        let mut before = base_user(Pubkey::new_unique());
        before.status = UserStatus::OutOfCredits;
        let mut after = before.clone();
        after.status = UserStatus::Activated;
        let changes = classify(&before, &after);

        let status = changes.iter().find(|(name, _)| *name == "status").unwrap();
        assert_eq!(
            status.1,
            FieldChange::ResetExpected {
                before: "OutOfCredits".to_string(),
                after: "Activated".to_string(),
            }
        );
    }

    // classify: tunnel_id differs -> ReallocatedChanged, not Unexpected.
    #[test]
    fn classify_tunnel_id_diff_is_reallocated_changed() {
        let before = base_user(Pubkey::new_unique());
        let mut after = before.clone();
        after.tunnel_id = before.tunnel_id + 1;
        let changes = classify(&before, &after);

        let tunnel_id = changes
            .iter()
            .find(|(name, _)| *name == "tunnel_id")
            .unwrap();
        assert_eq!(
            tunnel_id.1,
            FieldChange::ReallocatedChanged {
                before: before.tunnel_id.to_string(),
                after: after.tunnel_id.to_string(),
            }
        );
        assert!(
            !changes
                .iter()
                .any(|(_, c)| matches!(c, FieldChange::Unexpected { .. })),
            "no field should be Unexpected: {changes:?}"
        );
    }

    // classify: device_pk differs -> Unexpected.
    #[test]
    fn classify_device_pk_diff_is_unexpected() {
        let before = base_user(Pubkey::new_unique());
        let mut after = before.clone();
        after.device_pk = Pubkey::new_unique();
        let changes = classify(&before, &after);

        let device_pk = changes
            .iter()
            .find(|(name, _)| *name == "device_pk")
            .unwrap();
        assert_eq!(
            device_pk.1,
            FieldChange::Unexpected {
                before: format!("{:?}", before.device_pk),
                after: format!("{:?}", after.device_pk),
            }
        );
    }
}
