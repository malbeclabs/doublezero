use crate::{
    commands::{
        accesspass::get::GetAccessPassCommand, device::get::GetDeviceCommand,
        user::get::GetUserCommand,
    },
    doublezeroclient::SimulationOutcome,
    DoubleZeroClient,
};
use doublezero_serviceability::{
    processors::{
        multicastgroup::subscribe::UpdateMulticastGroupRolesArgs,
        user::{create::UserCreateArgs, delete::UserDeleteArgs},
    },
    state::user::User,
};
use doublezero_serviceability_instruction::{
    multicastgroup::update_multicast_group_roles,
    user::{create_user, delete_user},
};
use solana_sdk::{instruction::Instruction, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct RecreateUserCommand {
    pub pubkey: Pubkey,
}

/// The instruction sequence plus what is needed to interpret a simulation of it.
#[derive(Debug, PartialEq, Clone)]
pub struct RecreatePlan {
    pub instructions: Vec<Instruction>,
    pub user_pk: Pubkey,
    pub user_before: User,
}

impl RecreateUserCommand {
    /// Reads current state and assembles the unsubscribe/delete/create/resubscribe
    /// instruction sequence. Performs reads only, nothing is sent.
    pub fn plan(&self, client: &dyn DoubleZeroClient) -> eyre::Result<RecreatePlan> {
        let (user_pk, user) = GetUserCommand {
            pubkey: self.pubkey,
        }
        .execute(client)?;

        // The pass belongs to the user being rebuilt (user.owner), not necessarily the
        // payer sending this transaction.
        let (accesspass_pk, _) = GetAccessPassCommand {
            client_ip: user.client_ip,
            user_payer: user.owner,
        }
        .execute(client)?
        .ok_or_else(|| eyre::eyre!("You have no Access Pass"))?;

        let (_, device) = GetDeviceCommand {
            pubkey_or_code: user.device_pk.to_string(),
        }
        .execute(client)
        .map_err(|_| eyre::eyre!("Device not found"))?;
        let dz_prefix_count = device.dz_prefixes.len();
        if dz_prefix_count == 0 {
            return Err(eyre::eyre!(
                "Device {} has no dz_prefixes; cannot recreate user",
                user.device_pk
            ));
        }
        let dz_prefix_count_u8 = u8::try_from(dz_prefix_count).map_err(|_| {
            eyre::eyre!(
                "Device {} has {} dz_prefixes, exceeds u8::MAX",
                user.device_pk,
                dz_prefix_count
            )
        })?;

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let tenant = (user.tenant_pk != Pubkey::default()).then_some(user.tenant_pk);
        let groups = user.get_multicast_groups();

        // One removal and one re-addition per group, plus one delete and one create.
        let mut instructions = Vec::with_capacity(groups.len() * 2 + 2);

        // Removals precede the delete: UserDelete rejects a user still holding
        // publisher or subscriber roles. publisher: false, subscriber: false clears
        // both unconditionally, which is a safe no-op for a role the user never held.
        for group_pk in &groups {
            instructions.push(update_multicast_group_roles(
                &program_id,
                &payer,
                group_pk,
                &accesspass_pk,
                &user_pk,
                UpdateMulticastGroupRolesArgs {
                    client_ip: user.client_ip,
                    publisher: false,
                    subscriber: false,
                    use_onchain_allocation: true,
                },
            ));
        }

        instructions.push(delete_user(
            &program_id,
            &payer,
            &user_pk,
            &accesspass_pk,
            &user.device_pk,
            dz_prefix_count_u8,
            tenant,
            &user.owner,
            UserDeleteArgs {
                dz_prefix_count: dz_prefix_count_u8,
                multicast_publisher_count: 1,
            },
        ));

        instructions.push(create_user(
            &program_id,
            &payer,
            &user.device_pk,
            &accesspass_pk,
            dz_prefix_count_u8,
            tenant,
            UserCreateArgs {
                user_type: user.user_type,
                cyoa_type: user.cyoa_type,
                client_ip: user.client_ip,
                tunnel_endpoint: user.tunnel_endpoint,
                dz_prefix_count: dz_prefix_count_u8,
            },
        ));

        // Re-additions follow the create: the user account does not exist until then.
        // Roles are restored from user_before, so a group held as both publisher and
        // subscriber gets both flags back in a single instruction.
        for group_pk in &groups {
            instructions.push(update_multicast_group_roles(
                &program_id,
                &payer,
                group_pk,
                &accesspass_pk,
                &user_pk,
                UpdateMulticastGroupRolesArgs {
                    client_ip: user.client_ip,
                    publisher: user.publishers.contains(group_pk),
                    subscriber: user.subscribers.contains(group_pk),
                    use_onchain_allocation: true,
                },
            ));
        }

        Ok(RecreatePlan {
            instructions,
            user_pk,
            user_before: user,
        })
    }

    pub fn simulate(
        &self,
        client: &dyn DoubleZeroClient,
        plan: &RecreatePlan,
    ) -> eyre::Result<SimulationOutcome> {
        client.simulate_transaction_many(plan.instructions.clone(), vec![plan.user_pk])
    }

    pub fn send(
        &self,
        client: &dyn DoubleZeroClient,
        plan: &RecreatePlan,
    ) -> eyre::Result<Signature> {
        client.send_transaction_many(plan.instructions.clone())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_program_common::types::NetworkV4;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_accesspass_pda, get_user_pda},
        state::{
            accesspass::{AccessPass, AccessPassStatus, AccessPassType},
            accountdata::AccountData,
            accounttype::AccountType,
            device::Device,
            user::{UserCYOA, UserStatus, UserType},
        },
    };
    use mockall::predicate;
    use std::net::Ipv4Addr;

    /// Builds a client stubbed with `user`, its access pass (keyed on `user.owner`),
    /// and a device with exactly one dz_prefix. `user` is addressed at
    /// `get_user_pda(client_ip, user_type)`, the address `create_user` derives
    /// internally, so the returned pubkey matches what the create leg of a plan
    /// will target. Returns the client and that address.
    fn stub_client_for(
        user: &User,
        device_pubkey: Pubkey,
    ) -> (crate::MockDoubleZeroClient, Pubkey) {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let (user_pubkey, _) = get_user_pda(&program_id, &user.client_ip, user.user_type);

        let user_for_get = user.clone();
        client
            .expect_get()
            .with(predicate::eq(user_pubkey))
            .returning(move |_| Ok(AccountData::User(user_for_get.clone())));

        // GetAccessPassCommand checks the UNSPECIFIED (dynamic) PDA first. None exists
        // there in these fixtures, so it falls back to the exact-IP PDA.
        let (dynamic_accesspass_pubkey, _) =
            get_accesspass_pda(&program_id, &Ipv4Addr::UNSPECIFIED, &user.owner);
        client
            .expect_get()
            .with(predicate::eq(dynamic_accesspass_pubkey))
            .returning(|_| Err(eyre::eyre!("account not found")));

        let (accesspass_pubkey, _) = get_accesspass_pda(&program_id, &user.client_ip, &user.owner);
        let accesspass = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 0,
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user.client_ip,
            user_payer: user.owner,
            last_access_epoch: 0,
            connection_count: 0,
            status: AccessPassStatus::Requested,
            owner: user.owner,
            mgroup_pub_allowlist: user.publishers.clone(),
            mgroup_sub_allowlist: user.subscribers.clone(),
            tenant_allowlist: vec![],
            flags: 0,
            unicast_user_count: 0,
            max_unicast_users: 1,
            multicast_user_count: 0,
            max_multicast_users: 1,
        };
        client
            .expect_get()
            .with(predicate::eq(accesspass_pubkey))
            .returning(move |_| Ok(AccountData::AccessPass(accesspass.clone())));

        let device = Device {
            account_type: AccountType::Device,
            dz_prefixes: "10.0.0.0/24".parse().unwrap(),
            ..Default::default()
        };
        client
            .expect_get()
            .with(predicate::eq(device_pubkey))
            .returning(move |_| Ok(AccountData::Device(device.clone())));

        (client, user_pubkey)
    }

    /// Decode instruction `idx` and return its `UpdateMulticastGroupRoles` args,
    /// panicking with the actual variant on mismatch.
    fn expect_roles_args(
        instructions: &[Instruction],
        idx: usize,
    ) -> UpdateMulticastGroupRolesArgs {
        match DoubleZeroInstruction::unpack(&instructions[idx].data).unwrap() {
            DoubleZeroInstruction::UpdateMulticastGroupRoles(args) => args,
            other => {
                panic!("instruction[{idx}]: expected UpdateMulticastGroupRoles, got {other:?}")
            }
        }
    }

    /// The plan must order instructions unsubscribe->delete->create->resubscribe. The
    /// removals must precede the delete because UserDelete rejects a user holding
    /// publishers or subscribers. The re-additions must follow the create because the
    /// account does not exist until then.
    #[test]
    fn test_plan_orders_instructions_and_restores_roles() {
        let device_pubkey = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 10);
        let group_a = Pubkey::new_unique();
        let group_b = Pubkey::new_unique();

        let user = User {
            account_type: AccountType::User,
            owner,
            device_pk: device_pubkey,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![group_a, group_b],
            tunnel_endpoint: Ipv4Addr::new(10, 0, 0, 1),
            tunnel_flags: 0,
            tunnel_net: NetworkV4::default(),
            ..Default::default()
        };

        let (client, user_pubkey) = stub_client_for(&user, device_pubkey);
        let plan = RecreateUserCommand {
            pubkey: user_pubkey,
        }
        .plan(&client)
        .unwrap();

        assert_eq!(plan.instructions.len(), 6);

        // [0], [1]: removals for group_a and group_b, both roles cleared.
        for (idx, group) in [(0, group_a), (1, group_b)] {
            let args = expect_roles_args(&plan.instructions, idx);
            assert_eq!(plan.instructions[idx].accounts[0].pubkey, group);
            assert!(!args.publisher);
            assert!(!args.subscriber);
        }

        // [2]: UserDelete.
        match DoubleZeroInstruction::unpack(&plan.instructions[2].data).unwrap() {
            DoubleZeroInstruction::DeleteUser(_) => {}
            other => panic!("instruction[2]: expected DeleteUser, got {other:?}"),
        }

        // [3]: UserCreate, carrying the fetched user's identity fields.
        match DoubleZeroInstruction::unpack(&plan.instructions[3].data).unwrap() {
            DoubleZeroInstruction::CreateUser(args) => {
                assert_eq!(args.user_type, UserType::Multicast);
                assert_eq!(args.cyoa_type, UserCYOA::GREOverDIA);
                assert_eq!(args.client_ip, client_ip);
                assert_eq!(args.tunnel_endpoint, Ipv4Addr::new(10, 0, 0, 1));
            }
            other => panic!("instruction[3]: expected CreateUser, got {other:?}"),
        }

        // The delete leg and the create leg must target the same account: this is
        // the address-preservation invariant the whole feature exists to guarantee.
        assert_eq!(
            plan.instructions[2].accounts[0].pubkey,
            plan.instructions[3].accounts[0].pubkey
        );

        // [4], [5]: additions restoring subscriber = true, publisher = false.
        for (idx, group) in [(4, group_a), (5, group_b)] {
            let args = expect_roles_args(&plan.instructions, idx);
            assert_eq!(plan.instructions[idx].accounts[0].pubkey, group);
            assert!(!args.publisher);
            assert!(args.subscriber);
        }

        assert_eq!(plan.user_pk, user_pubkey);
        assert_eq!(plan.user_before, user);
    }

    /// A user with no multicast membership degenerates to the two-instruction case.
    #[test]
    fn test_plan_without_multicast_is_delete_then_create() {
        let device_pubkey = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 20);

        let user = User {
            account_type: AccountType::User,
            owner,
            device_pk: device_pubkey,
            user_type: UserType::IBRLWithAllocatedIP,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            status: UserStatus::Activated,
            publishers: vec![],
            subscribers: vec![],
            ..Default::default()
        };

        let (client, user_pubkey) = stub_client_for(&user, device_pubkey);
        let plan = RecreateUserCommand {
            pubkey: user_pubkey,
        }
        .plan(&client)
        .unwrap();

        assert_eq!(plan.instructions.len(), 2);
        match DoubleZeroInstruction::unpack(&plan.instructions[0].data).unwrap() {
            DoubleZeroInstruction::DeleteUser(_) => {}
            other => panic!("instruction[0]: expected DeleteUser, got {other:?}"),
        }
        match DoubleZeroInstruction::unpack(&plan.instructions[1].data).unwrap() {
            DoubleZeroInstruction::CreateUser(_) => {}
            other => panic!("instruction[1]: expected CreateUser, got {other:?}"),
        }

        // The delete leg and the create leg must target the same account: this is
        // the address-preservation invariant the whole feature exists to guarantee.
        assert_eq!(
            plan.instructions[0].accounts[0].pubkey,
            plan.instructions[1].accounts[0].pubkey
        );
    }

    /// A group the user both publishes to and subscribes to must have both roles
    /// restored, not just one.
    #[test]
    fn test_plan_restores_dual_role_group() {
        let device_pubkey = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let client_ip = Ipv4Addr::new(192, 168, 1, 30);
        let group_a = Pubkey::new_unique();

        let user = User {
            account_type: AccountType::User,
            owner,
            device_pk: device_pubkey,
            user_type: UserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip,
            status: UserStatus::Activated,
            publishers: vec![group_a],
            subscribers: vec![group_a],
            tunnel_flags: 0,
            ..Default::default()
        };

        let (client, user_pubkey) = stub_client_for(&user, device_pubkey);
        let plan = RecreateUserCommand {
            pubkey: user_pubkey,
        }
        .plan(&client)
        .unwrap();

        // get_multicast_groups() dedupes publishers+subscribers to a single entry, so
        // this is: 1 removal, delete, create, 1 (dual-role) addition.
        assert_eq!(plan.instructions.len(), 4);

        // The delete leg and the create leg must target the same account: this is
        // the address-preservation invariant the whole feature exists to guarantee.
        assert_eq!(
            plan.instructions[1].accounts[0].pubkey,
            plan.instructions[2].accounts[0].pubkey
        );

        let removal_args = expect_roles_args(&plan.instructions, 0);
        assert_eq!(plan.instructions[0].accounts[0].pubkey, group_a);
        assert!(!removal_args.publisher);
        assert!(!removal_args.subscriber);

        let addition_args = expect_roles_args(&plan.instructions, 3);
        assert_eq!(plan.instructions[3].accounts[0].pubkey, group_a);
        assert!(addition_args.publisher);
        assert!(addition_args.subscriber);
    }
}
