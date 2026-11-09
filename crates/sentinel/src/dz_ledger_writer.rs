use std::net::Ipv4Addr;

use anyhow::{ensure, Result};
use doublezero_serviceability::{
    pda::get_accesspass_pda,
    processors::{
        accesspass::set::SetAccessPassArgs,
        multicastgroup::allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs,
        user::create_subscribe::UserCreateSubscribeArgs,
    },
    state::{
        accesspass::AccessPassType,
        user::{UserCYOA, UserType as SvcUserType},
    },
};
use doublezero_serviceability_instruction as svc_ix;
use solana_sdk::{instruction::Instruction, pubkey::Pubkey};

use super::dz_ledger_reader::DzUser;

/// Puts the compute-budget prelude (protocol-max compute-unit limit and heap frame) in front of
/// `instructions`, as every serviceability transaction should carry it. The program allocates
/// from the heap frame the transaction requests; without one, an instruction that loads a device
/// with many interfaces runs out of the default 32 KiB.
pub fn with_compute_budget_prelude(instructions: &[Instruction]) -> Vec<Instruction> {
    let mut ixs = svc_ix::compute_budget_prelude().to_vec();
    ixs.extend_from_slice(instructions);
    ixs
}

/// The three instructions needed to create a multicast publisher onchain.
pub struct CreateMulticastPublisherInstructions {
    pub set_access_pass: Instruction,
    pub add_allowlist: Instruction,
    pub create_user: Instruction,
}

/// Build the three instructions needed to create a multicast publisher for a user.
///
/// `payer` signs and pays for all transactions. `owner` is the validator's owner pubkey;
/// the access pass and user account are created under this owner, not the payer.
///
/// `dz_prefix_count` is the number of DzPrefixBlock resource extension accounts on the
/// device (i.e. `device.dz_prefixes.len()`). It must be > 0: CreateSubscribeUser only runs
/// the atomic create+allocate+activate path, and every DzPrefixBlock must be supplied even
/// though a multicast publisher allocates its dz_ip from MulticastPublisherBlock.
pub fn build_create_multicast_publisher_instructions(
    program_id: &Pubkey,
    payer: &Pubkey,
    owner: &Pubkey,
    multicast_group_pk: &Pubkey,
    user: &DzUser,
    tunnel_endpoint: Ipv4Addr,
    dz_prefix_count: u8,
) -> Result<CreateMulticastPublisherInstructions> {
    // The program rejects CreateSubscribeUser with no DzPrefixBlock accounts, so fail before
    // issuing an access pass and allowlist entry for a user that cannot be created.
    ensure!(
        dz_prefix_count > 0,
        "device {} has no dz_prefixes; CreateSubscribeUser requires onchain allocation",
        user.device_pk
    );

    let (accesspass_pda, _) = get_accesspass_pda(program_id, &user.client_ip, owner);

    // Step 1: set_access_pass
    let set_access_pass = svc_ix::accesspass::set_access_pass(
        program_id,
        payer,
        owner,
        // No tenant change: default keys make the builder omit both tenant slots.
        &Pubkey::default(),
        &Pubkey::default(),
        SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user.client_ip,
            last_access_epoch: u64::MAX,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        },
    );

    // Step 2: add_multicast_publisher_allowlist
    let add_allowlist = svc_ix::multicastgroup::add_multicast_group_pub_allowlist(
        program_id,
        payer,
        multicast_group_pk,
        &accesspass_pda,
        AddMulticastGroupPubAllowlistArgs {
            client_ip: user.client_ip,
            user_payer: *owner,
        },
    );

    // Step 3: create_subscribe_user (as publisher), with the ResourceExtension accounts the
    // atomic create+allocate+activate path needs.
    let create_user = svc_ix::user::create_subscribe_user(
        program_id,
        payer,
        &user.device_pk,
        multicast_group_pk,
        &accesspass_pda,
        dz_prefix_count,
        &[],
        None,
        UserCreateSubscribeArgs {
            user_type: SvcUserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user.client_ip,
            publisher: true,
            subscriber: false,
            tunnel_endpoint,
            dz_prefix_count,
            owner: *owner,
            ip_proof: None,
            extra_group_count: 0,
        },
    );

    Ok(CreateMulticastPublisherInstructions {
        set_access_pass,
        add_allowlist,
        create_user,
    })
}

#[cfg(test)]
mod tests {
    use std::net::Ipv4Addr;

    use doublezero_sdk::UserType;
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_resource_extension_pda, get_user_pda},
        resource::ResourceType,
    };
    use solana_sdk::instruction::AccountMeta;

    use super::*;

    fn test_user() -> DzUser {
        DzUser {
            owner: Pubkey::new_unique(),
            client_ip: Ipv4Addr::new(10, 0, 0, 1),
            device_pk: Pubkey::new_unique(),
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            publishers: vec![],
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        }
    }

    /// The instruction-specific accounts plus the signing payer, in processor order. The
    /// builders append further trailing accounts (the system program and the payer's
    /// Permission PDA), so only this prefix is pinned here.
    fn assert_leading_accounts(ix: &Instruction, expected: &[AccountMeta]) {
        assert_eq!(&ix.accounts[..expected.len()], expected);
    }

    #[test]
    fn build_instructions_match_instruction_builders() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let owner = Pubkey::new_unique();
        let multicast_group = Pubkey::new_unique();
        let user = test_user();
        let tunnel_endpoint = Ipv4Addr::new(45, 33, 101, 1);
        let dz_prefix_count: u8 = 2;

        let ixs = build_create_multicast_publisher_instructions(
            &program_id,
            &payer,
            &owner,
            &multicast_group,
            &user,
            tunnel_endpoint,
            dz_prefix_count,
        )
        .unwrap();

        let (accesspass, _) = get_accesspass_pda(&program_id, &user.client_ip, &owner);
        let (globalstate, _) = get_globalstate_pda(&program_id);

        // Step 1: SetAccessPass, no tenant slots.
        let set_args = SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user.client_ip,
            last_access_epoch: u64::MAX,
            allow_multiple_ip: false,
            max_unicast_users: 1,
            max_multicast_users: 1,
        };
        assert_eq!(
            ixs.set_access_pass,
            svc_ix::accesspass::set_access_pass(
                &program_id,
                &payer,
                &owner,
                &Pubkey::default(),
                &Pubkey::default(),
                set_args.clone(),
            )
        );
        assert_eq!(
            ixs.set_access_pass.data,
            DoubleZeroInstruction::SetAccessPass(set_args).pack()
        );
        assert_leading_accounts(
            &ixs.set_access_pass,
            &[
                AccountMeta::new(accesspass, false),
                AccountMeta::new_readonly(globalstate, false),
                AccountMeta::new(owner, false),
                AccountMeta::new(payer, true),
            ],
        );

        // Step 2: AddMulticastGroupPubAllowlist, with the optional user_payer account.
        let allow_args = AddMulticastGroupPubAllowlistArgs {
            client_ip: user.client_ip,
            user_payer: owner,
        };
        assert_eq!(
            ixs.add_allowlist,
            svc_ix::multicastgroup::add_multicast_group_pub_allowlist(
                &program_id,
                &payer,
                &multicast_group,
                &accesspass,
                allow_args.clone(),
            )
        );
        assert_eq!(
            ixs.add_allowlist.data,
            DoubleZeroInstruction::AddMulticastGroupPubAllowlist(allow_args).pack()
        );
        assert_leading_accounts(
            &ixs.add_allowlist,
            &[
                AccountMeta::new(multicast_group, false),
                AccountMeta::new(accesspass, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(owner, false),
                AccountMeta::new(payer, true),
            ],
        );

        // Step 3: CreateSubscribeUser through the onchain allocation path.
        let create_args = UserCreateSubscribeArgs {
            user_type: SvcUserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user.client_ip,
            publisher: true,
            subscriber: false,
            tunnel_endpoint,
            dz_prefix_count,
            owner,
            ip_proof: None,
            extra_group_count: 0,
        };
        assert_eq!(
            ixs.create_user,
            svc_ix::user::create_subscribe_user(
                &program_id,
                &payer,
                &user.device_pk,
                &multicast_group,
                &accesspass,
                dz_prefix_count,
                &[],
                None,
                create_args.clone(),
            )
        );
        assert_eq!(
            ixs.create_user.data,
            DoubleZeroInstruction::CreateSubscribeUser(create_args).pack()
        );
        let (user_pda, _) = get_user_pda(&program_id, &user.client_ip, SvcUserType::Multicast);
        let ext = |rt| get_resource_extension_pda(&program_id, rt).0;
        let mut expected = vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new(user.device_pk, false),
            AccountMeta::new(multicast_group, false),
            AccountMeta::new(accesspass, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(ext(ResourceType::UserTunnelBlock), false),
            AccountMeta::new(ext(ResourceType::MulticastPublisherBlock), false),
            AccountMeta::new(ext(ResourceType::TunnelIds(user.device_pk, 0)), false),
        ];
        for idx in 0..dz_prefix_count as usize {
            expected.push(AccountMeta::new(
                ext(ResourceType::DzPrefixBlock(user.device_pk, idx)),
                false,
            ));
        }
        expected.push(AccountMeta::new(payer, true));
        assert_leading_accounts(&ixs.create_user, &expected);
    }

    #[test]
    fn build_instructions_rejects_device_without_dz_prefixes() {
        let user = test_user();
        let err = build_create_multicast_publisher_instructions(
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            &Pubkey::new_unique(),
            &user,
            Ipv4Addr::UNSPECIFIED,
            0,
        )
        .err()
        .expect("dz_prefix_count = 0 must be rejected");
        assert_eq!(
            err.to_string(),
            format!(
                "device {} has no dz_prefixes; CreateSubscribeUser requires onchain allocation",
                user.device_pk
            )
        );
    }
}
