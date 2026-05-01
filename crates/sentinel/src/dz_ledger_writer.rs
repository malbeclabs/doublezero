use std::net::Ipv4Addr;

use anyhow::{Context, Result};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_accesspass_pda, get_globalstate_pda, get_resource_extension_pda, get_user_pda},
    processors::{
        accesspass::set::SetAccessPassArgs,
        multicastgroup::allowlist::publisher::add::AddMulticastGroupPubAllowlistArgs,
        user::create_subscribe::UserCreateSubscribeArgs,
    },
    resource::ResourceType,
    state::{
        accesspass::AccessPassType,
        user::{UserCYOA, UserType as SvcUserType},
    },
};
use solana_sdk::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

use super::dz_ledger_reader::DzUser;

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
/// device (i.e. `device.dz_prefixes.len()`). When > 0 the create_subscribe_user instruction
/// runs through the atomic create+allocate+activate path; every DzPrefixBlock must be
/// supplied even though a multicast publisher allocates its dz_ip from MulticastPublisherBlock.
pub fn build_create_multicast_publisher_instructions(
    program_id: &Pubkey,
    payer: &Pubkey,
    owner: &Pubkey,
    multicast_group_pk: &Pubkey,
    user: &DzUser,
    tunnel_endpoint: Ipv4Addr,
    dz_prefix_count: u8,
) -> Result<CreateMulticastPublisherInstructions> {
    let (accesspass_pda, _) = get_accesspass_pda(program_id, &user.client_ip, owner);
    let (globalstate_pda, _) = get_globalstate_pda(program_id);

    // Step 1: set_access_pass
    let set_access_pass = build_instruction(
        program_id,
        DoubleZeroInstruction::SetAccessPass(SetAccessPassArgs {
            accesspass_type: AccessPassType::Prepaid,
            client_ip: user.client_ip,
            last_access_epoch: u64::MAX,
            allow_multiple_ip: false,
        }),
        vec![
            AccountMeta::new(accesspass_pda, false),
            AccountMeta::new_readonly(globalstate_pda, false),
            AccountMeta::new(*owner, false),
            AccountMeta::new(*payer, true),
            AccountMeta::new_readonly(solana_sdk::system_program::ID, false),
        ],
    )?;

    // Step 2: add_multicast_publisher_allowlist
    let add_allowlist = build_instruction(
        program_id,
        DoubleZeroInstruction::AddMulticastGroupPubAllowlist(AddMulticastGroupPubAllowlistArgs {
            client_ip: user.client_ip,
            user_payer: *owner,
        }),
        vec![
            AccountMeta::new(*multicast_group_pk, false),
            AccountMeta::new(accesspass_pda, false),
            AccountMeta::new_readonly(globalstate_pda, false),
            AccountMeta::new(*payer, true),
            AccountMeta::new_readonly(solana_sdk::system_program::ID, false),
        ],
    )?;

    // Step 3: create_subscribe_user (as publisher).
    //
    // When dz_prefix_count > 0 we append the ResourceExtension accounts so the contract takes
    // the atomic create+allocate+activate path. Without those accounts the user is created
    // Pending and — with no activator — never reaches Activated, so the next poll cycle would
    // re-attempt creation and trip AccountAlreadyInitialized.
    let (user_pda, _) = get_user_pda(program_id, &user.client_ip, SvcUserType::Multicast);
    let mut create_user_accounts = vec![
        AccountMeta::new(user_pda, false),
        AccountMeta::new(user.device_pk, false),
        AccountMeta::new(*multicast_group_pk, false),
        AccountMeta::new(accesspass_pda, false),
        AccountMeta::new(globalstate_pda, false),
    ];

    if dz_prefix_count > 0 {
        let (user_tunnel_block_ext, _, _) =
            get_resource_extension_pda(program_id, ResourceType::UserTunnelBlock);
        let (multicast_publisher_block_ext, _, _) =
            get_resource_extension_pda(program_id, ResourceType::MulticastPublisherBlock);
        let (device_tunnel_ids_ext, _, _) =
            get_resource_extension_pda(program_id, ResourceType::TunnelIds(user.device_pk, 0));

        create_user_accounts.push(AccountMeta::new(user_tunnel_block_ext, false));
        create_user_accounts.push(AccountMeta::new(multicast_publisher_block_ext, false));
        create_user_accounts.push(AccountMeta::new(device_tunnel_ids_ext, false));

        for idx in 0..dz_prefix_count as usize {
            let (dz_prefix_ext, _, _) = get_resource_extension_pda(
                program_id,
                ResourceType::DzPrefixBlock(user.device_pk, idx),
            );
            create_user_accounts.push(AccountMeta::new(dz_prefix_ext, false));
        }
    }

    create_user_accounts.push(AccountMeta::new(*payer, true));
    create_user_accounts.push(AccountMeta::new_readonly(
        solana_sdk::system_program::ID,
        false,
    ));

    let create_user = build_instruction(
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: SvcUserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user.client_ip,
            publisher: true,
            subscriber: false,
            tunnel_endpoint,
            dz_prefix_count,
            owner: *owner,
        }),
        create_user_accounts,
    )?;

    Ok(CreateMulticastPublisherInstructions {
        set_access_pass,
        add_allowlist,
        create_user,
    })
}

fn build_instruction(
    program_id: &Pubkey,
    dz_ix: DoubleZeroInstruction,
    accounts: Vec<AccountMeta>,
) -> Result<Instruction> {
    let data = borsh::to_vec(&dz_ix).context("failed to serialize instruction")?;
    Ok(Instruction {
        program_id: *program_id,
        accounts,
        data,
    })
}

#[cfg(test)]
mod tests {
    use std::net::Ipv4Addr;

    use doublezero_sdk::UserType;

    use super::*;

    #[test]
    fn build_instructions_returns_three_instructions() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let multicast_group = Pubkey::new_unique();
        let user = DzUser {
            owner: Pubkey::new_unique(),
            client_ip: Ipv4Addr::new(10, 0, 0, 1),
            device_pk: Pubkey::new_unique(),
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            publishers: vec![],
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let owner = Pubkey::new_unique();

        let ixs = build_create_multicast_publisher_instructions(
            &program_id,
            &payer,
            &owner,
            &multicast_group,
            &user,
            Ipv4Addr::UNSPECIFIED,
            0,
        )
        .unwrap();

        // All three instructions target the correct program.
        assert_eq!(ixs.set_access_pass.program_id, program_id);
        assert_eq!(ixs.add_allowlist.program_id, program_id);
        assert_eq!(ixs.create_user.program_id, program_id);

        // Each has non-empty data (serialized instruction).
        assert!(!ixs.set_access_pass.data.is_empty());
        assert!(!ixs.add_allowlist.data.is_empty());
        assert!(!ixs.create_user.data.is_empty());

        // set_access_pass: 5 accounts (accesspass_pda, globalstate, owner, payer, system_program)
        assert_eq!(ixs.set_access_pass.accounts.len(), 5);
        // add_allowlist: 5 accounts (multicast_group, accesspass_pda, globalstate, payer, system_program)
        assert_eq!(ixs.add_allowlist.accounts.len(), 5);
        // create_user (legacy, dz_prefix_count=0): 7 accounts
        // (user_pda, device, multicast_group, accesspass_pda, globalstate, payer, system_program)
        assert_eq!(ixs.create_user.accounts.len(), 7);
    }

    #[test]
    fn build_instructions_with_onchain_allocation() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let multicast_group = Pubkey::new_unique();
        let user = DzUser {
            owner: Pubkey::new_unique(),
            client_ip: Ipv4Addr::new(10, 0, 0, 1),
            device_pk: Pubkey::new_unique(),
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            publishers: vec![],
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let owner = Pubkey::new_unique();
        let dz_prefix_count: u8 = 2;

        let ixs = build_create_multicast_publisher_instructions(
            &program_id,
            &payer,
            &owner,
            &multicast_group,
            &user,
            Ipv4Addr::new(45, 33, 101, 1),
            dz_prefix_count,
        )
        .unwrap();

        // create_user with onchain allocation: 7 base accounts + 3 (UserTunnelBlock,
        // MulticastPublisherBlock, TunnelIds) + dz_prefix_count DzPrefixBlock accounts.
        assert_eq!(
            ixs.create_user.accounts.len(),
            7 + 3 + dz_prefix_count as usize
        );
        // payer must still be the signer regardless of where it lands in the account list.
        assert!(ixs
            .create_user
            .accounts
            .iter()
            .any(|a| a.pubkey == payer && a.is_signer));
    }

    #[test]
    fn payer_is_signer_in_all_instructions() {
        let program_id = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let multicast_group = Pubkey::new_unique();
        let user = DzUser {
            owner: Pubkey::new_unique(),
            client_ip: Ipv4Addr::new(10, 0, 0, 1),
            device_pk: Pubkey::new_unique(),
            tenant_pk: Pubkey::default(),
            user_type: UserType::IBRL,
            publishers: vec![],
            tunnel_endpoint: Ipv4Addr::UNSPECIFIED,
        };

        let owner = Pubkey::new_unique();

        let ixs = build_create_multicast_publisher_instructions(
            &program_id,
            &payer,
            &owner,
            &multicast_group,
            &user,
            Ipv4Addr::UNSPECIFIED,
            0,
        )
        .unwrap();

        for ix in [&ixs.set_access_pass, &ixs.add_allowlist, &ixs.create_user] {
            assert!(
                ix.accounts.iter().any(|a| a.pubkey == payer && a.is_signer),
                "payer should be a signer"
            );
        }
    }
}
