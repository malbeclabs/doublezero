use anyhow::{Context, Result};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_accesspass_pda, get_globalstate_pda, get_user_pda},
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
pub fn build_create_multicast_publisher_instructions(
    program_id: &Pubkey,
    payer: &Pubkey,
    owner: &Pubkey,
    multicast_group_pk: &Pubkey,
    user: &DzUser,
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

    // Step 3: create_subscribe_user (as publisher)
    let (user_pda, _) = get_user_pda(program_id, &user.client_ip, SvcUserType::Multicast);
    let create_user = build_instruction(
        program_id,
        DoubleZeroInstruction::CreateSubscribeUser(UserCreateSubscribeArgs {
            user_type: SvcUserType::Multicast,
            cyoa_type: UserCYOA::GREOverDIA,
            client_ip: user.client_ip,
            publisher: true,
            subscriber: false,
            tunnel_endpoint: std::net::Ipv4Addr::UNSPECIFIED,
            dz_prefix_count: 0,
            owner: *owner,
        }),
        vec![
            AccountMeta::new(user_pda, false),
            AccountMeta::new(user.device_pk, false),
            AccountMeta::new(*multicast_group_pk, false),
            AccountMeta::new(accesspass_pda, false),
            AccountMeta::new_readonly(globalstate_pda, false),
            AccountMeta::new(*payer, true),
            AccountMeta::new_readonly(solana_sdk::system_program::ID, false),
        ],
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
        };

        let owner = Pubkey::new_unique();

        let ixs = build_create_multicast_publisher_instructions(
            &program_id,
            &payer,
            &owner,
            &multicast_group,
            &user,
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
        // create_user: 7 accounts (user_pda, device, multicast_group, accesspass_pda, globalstate, payer, system_program)
        assert_eq!(ixs.create_user.accounts.len(), 7);
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
        };

        let owner = Pubkey::new_unique();

        let ixs = build_create_multicast_publisher_instructions(
            &program_id,
            &payer,
            &owner,
            &multicast_group,
            &user,
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
