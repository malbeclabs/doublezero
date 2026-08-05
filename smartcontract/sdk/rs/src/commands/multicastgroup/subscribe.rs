use std::net::Ipv4Addr;

use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, multicastgroup::get::GetMulticastGroupCommand,
        user::get::GetUserCommand,
    },
    DoubleZeroClient,
};
use doublezero_serviceability::{
    processors::multicastgroup::subscribe::UpdateMulticastGroupRolesArgs,
    state::multicastgroup::MulticastGroupStatus,
};
use doublezero_serviceability_instruction::multicastgroup::update_multicast_group_roles;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

/// Upper bound on multicast groups per role-update transaction. Each group adds a
/// 32-byte account key; 16 groups plus the instruction's fixed accounts, the
/// compute-budget prelude, and an optional Permission PDA stay comfortably under
/// the 1232-byte transaction size limit (pinned by `max_group_batch_fits_transaction`).
/// Callers with more groups send one transaction per chunk.
pub const MAX_GROUPS_PER_TRANSACTION: usize = 16;

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateMulticastGroupRolesCommand {
    /// Multicast groups the role change applies to, atomically in one transaction.
    /// Must be non-empty; the first entry is the instruction's primary group and the
    /// rest ride as extra group accounts.
    pub group_pks: Vec<Pubkey>,
    pub client_ip: Ipv4Addr,
    pub user_pk: Pubkey,
    pub publisher: bool,
    pub subscriber: bool,
    /// Reserved for the EdgeSeat feed metro gate (the user's device + covering Feed). Not appended
    /// by this builder — the authorized-transaction layout has no slot after the trailing
    /// `[payer, system, permission]`; post-activation re-gating is deferred to #1699.
    pub device_pk: Option<Pubkey>,
    pub feed_pk: Option<Pubkey>,
}

impl UpdateMulticastGroupRolesCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        // Deduplicate while preserving order: the processor rejects duplicate group
        // accounts in a batch, and a repeated group was an idempotent no-op under
        // the old per-group loop.
        let mut group_pks: Vec<Pubkey> = Vec::with_capacity(self.group_pks.len());
        for pk in &self.group_pks {
            if !group_pks.contains(pk) {
                group_pks.push(*pk);
            }
        }
        if group_pks.len() > MAX_GROUPS_PER_TRANSACTION {
            eyre::bail!(
                "{} multicast groups exceed the {MAX_GROUPS_PER_TRANSACTION}-group transaction limit; send one transaction per chunk",
                group_pks.len()
            );
        }
        let (first_group_pk, extra_group_pks) = group_pks
            .split_first()
            .ok_or_else(|| eyre::eyre!("At least one multicast group is required"))?;

        for group_pk in &group_pks {
            let (_, mgroup) = GetMulticastGroupCommand {
                pubkey_or_code: group_pk.to_string(),
            }
            .execute(client)
            .map_err(|_err| eyre::eyre!("MulticastGroup not found ({group_pk})"))?;

            if mgroup.status != MulticastGroupStatus::Activated {
                eyre::bail!("MulticastGroup not active ({group_pk})");
            }
        }

        let (_, user) = GetUserCommand {
            pubkey: self.user_pk,
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("User not found"))?;

        // GetAccessPassCommand prefers a shared dynamic (UNSPECIFIED) pass and falls
        // back to the exact client-IP pass.
        let (accesspass_pubkey, accesspass) = GetAccessPassCommand {
            client_ip: self.client_ip,
            user_payer: user.owner,
        }
        .execute(client)?
        .ok_or_else(|| eyre::eyre!("AccessPass not found"))?;

        for group_pk in &group_pks {
            if self.publisher && !accesspass.mgroup_pub_allowlist.contains(group_pk) {
                eyre::bail!("User not allowed to publish multicast group ({group_pk})");
            }
            if self.subscriber && !accesspass.mgroup_sub_allowlist.contains(group_pk) {
                eyre::bail!("User not allowed to subscribe multicast group ({group_pk})");
            }
        }

        // The EdgeSeat feed metro gate is enforced at connect (CreateSubscribeUser). The optional
        // `device_pk`/`feed_pk` for post-activation re-gating are NOT passed to the builder here:
        // the `update_multicast_group_roles` layout has no slot for them via this path.
        // Post-activation re-gating is deferred to the oracle lifecycle
        // (see malbeclabs/infra#1700 / doublezero #1699).
        client.send_transaction(update_multicast_group_roles(
            &client.get_program_id(),
            &client.get_payer(),
            first_group_pk,
            &accesspass_pubkey,
            &self.user_pk,
            extra_group_pks,
            UpdateMulticastGroupRolesArgs {
                publisher: self.publisher,
                subscriber: self.subscriber,
                client_ip: user.client_ip,
                use_onchain_allocation: true,
                extra_group_count: 0, // derived by the builder from extra_group_pks
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::subscribe::UpdateMulticastGroupRolesCommand,
        tests::utils::create_test_client, DoubleZeroClient,
    };
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        pda::{get_accesspass_pda, get_multicastgroup_pda},
        processors::multicastgroup::subscribe::UpdateMulticastGroupRolesArgs,
        state::{
            accountdata::AccountData,
            accounttype::AccountType,
            multicastgroup::{MulticastGroup, MulticastGroupStatus},
            user::{User, UserCYOA, UserStatus, UserType},
        },
    };
    use doublezero_serviceability_instruction::multicastgroup::update_multicast_group_roles;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::net::Ipv4Addr;

    #[test]
    fn test_commands_multicastgroup_subscribe_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let (mgroup_pubkey, _bump_seed) = get_multicastgroup_pda(&program_id, 1);
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            owner: payer,
            bump_seed: 0,
            index: 1,
            code: "test".to_string(),
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            tenant_pk: Pubkey::default(),
            multicast_ip: "223.0.0.1".parse().unwrap(),
            publisher_count: 0,
            subscriber_count: 0,
        };

        client
            .expect_get()
            .with(predicate::eq(mgroup_pubkey))
            .returning(move |_| Ok(AccountData::MulticastGroup(mgroup.clone())));

        let client_ip = Ipv4Addr::new(192, 168, 1, 10);

        let user_pubkey = Pubkey::new_unique();
        let user = User {
            account_type: AccountType::User,
            owner: payer,
            bump_seed: 0,
            index: 1,
            tenant_pk: Pubkey::default(),
            user_type: UserType::Multicast,
            device_pk: mgroup_pubkey,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            dz_ip: client_ip,
            tunnel_id: 0,
            tunnel_net: NetworkV4::default(),
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            validator_pubkey: Pubkey::default(),
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
            tunnel_flags: 0,
            bgp_status: Default::default(),
            last_bgp_up_at: 0,
            last_bgp_reported_at: 0,
            bgp_rtt_ns: 0,
            ..Default::default()
        };

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user.client_ip, &payer);
        let accesspass = doublezero_serviceability::state::accesspass::AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: doublezero_serviceability::state::accesspass::AccessPassType::Prepaid,
            client_ip: user.client_ip,
            user_payer: payer,
            last_access_epoch: 0,
            connection_count: 0,
            status: doublezero_serviceability::state::accesspass::AccessPassStatus::Requested,
            owner: payer,
            mgroup_pub_allowlist: vec![mgroup_pubkey],
            mgroup_sub_allowlist: vec![mgroup_pubkey],
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };

        // First call in UpdateMulticastGroupRolesCommand::execute tries the dynamic (UNSPECIFIED) PDA,
        // which should fail with a non-AccessPass to trigger the fallback to the fixed client_ip PDA.
        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &payer);
        let user_clone_for_dynamic = user.clone();
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning(move |_| Ok(AccountData::User(user_clone_for_dynamic.clone())));

        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user.clone())));

        let expected = update_multicast_group_roles(
            &program_id,
            &payer,
            &mgroup_pubkey,
            &accesspass_pubkey,
            &user_pubkey,
            &[],
            UpdateMulticastGroupRolesArgs {
                client_ip,
                publisher: true,
                subscriber: false,
                use_onchain_allocation: true,
                extra_group_count: 0,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = UpdateMulticastGroupRolesCommand {
            group_pks: vec![mgroup_pubkey],
            user_pk: user_pubkey,
            client_ip,
            publisher: true,
            subscriber: false,
            device_pk: None,
            feed_pk: None,
        }
        .execute(&client);

        assert!(res.is_ok());
    }

    /// A max-size batch (MAX_GROUPS_PER_TRANSACTION groups + Permission PDA) must fit
    /// the 1232-byte transaction size limit, including the compute-budget prelude the
    /// SDK prepends.
    #[test]
    fn max_group_batch_fits_transaction_size() {
        use solana_sdk::{instruction::AccountMeta, message::Message};

        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let group_pks: Vec<Pubkey> = (0
            ..crate::commands::multicastgroup::subscribe::MAX_GROUPS_PER_TRANSACTION)
            .map(|_| Pubkey::new_unique())
            .collect();
        let mut ix = update_multicast_group_roles(
            &program_id,
            &payer,
            &group_pks[0],
            &Pubkey::new_unique(), // accesspass
            &Pubkey::new_unique(), // user
            &group_pks[1..],
            UpdateMulticastGroupRolesArgs {
                client_ip: std::net::Ipv4Addr::new(1, 2, 3, 4),
                publisher: true,
                subscriber: true,
                use_onchain_allocation: true,
                extra_group_count: 0,
            },
        );
        // Worst case also carries the payer's Permission PDA.
        ix.accounts
            .push(AccountMeta::new_readonly(Pubkey::new_unique(), false));

        let message = Message::new(&[ix], Some(&payer));
        // Wire size: 1-byte signature count + 64 bytes per signature + the message.
        let tx_size =
            1 + 64 * message.header.num_required_signatures as usize + message.serialize().len();
        // The SDK's send_transaction prepends two compute-budget instructions
        // (one program key + two short instructions), comfortably under this margin.
        const COMPUTE_BUDGET_PRELUDE_MARGIN: usize = 100;
        assert!(
            tx_size + COMPUTE_BUDGET_PRELUDE_MARGIN <= 1232,
            "max batch transaction is {tx_size} bytes + {COMPUTE_BUDGET_PRELUDE_MARGIN} margin, over the 1232-byte limit"
        );
    }
}
