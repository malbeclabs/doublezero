//! Exchange-domain instruction builders.
//!
//! CRUD template like `location`, with two wrinkles: `create`/`update` also take
//! the globalconfig account, and `set_device_exchange` takes a device. All route
//! through `authorize()` -> [`common::build_with_permission`].

use crate::common;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::{get_exchange_pda, get_globalconfig_pda, get_globalstate_pda},
    processors::exchange::{
        create::ExchangeCreateArgs, delete::ExchangeDeleteArgs, resume::ExchangeResumeArgs,
        setdevice::ExchangeSetDeviceArgs, suspend::ExchangeSuspendArgs, update::ExchangeUpdateArgs,
    },
};
use solana_program::{
    instruction::{AccountMeta, Instruction},
    pubkey::Pubkey,
};

/// `CreateExchange` (variant 15). Accounts: `[exchange, globalconfig, globalstate]`.
///
/// `account_index` is the new exchange's index (`globalstate.account_index + 1`).
pub fn create_exchange(
    program_id: &Pubkey,
    payer: &Pubkey,
    account_index: u128,
    args: ExchangeCreateArgs,
) -> Instruction {
    let (exchange, _) = get_exchange_pda(program_id, account_index);
    let (globalconfig, _) = get_globalconfig_pda(program_id);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::CreateExchange(args),
        vec![
            AccountMeta::new(exchange, false),
            AccountMeta::new(globalconfig, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `UpdateExchange` (variant 16). Accounts: `[exchange, globalconfig, globalstate]`.
pub fn update_exchange(
    program_id: &Pubkey,
    payer: &Pubkey,
    exchange: &Pubkey,
    args: ExchangeUpdateArgs,
) -> Instruction {
    let (globalconfig, _) = get_globalconfig_pda(program_id);
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::UpdateExchange(args),
        vec![
            AccountMeta::new(*exchange, false),
            AccountMeta::new(globalconfig, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `SuspendExchange` (variant 17). Accounts: `[exchange, globalstate]`.
pub fn suspend_exchange(
    program_id: &Pubkey,
    payer: &Pubkey,
    exchange: &Pubkey,
    args: ExchangeSuspendArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SuspendExchange(args),
        vec![
            AccountMeta::new(*exchange, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `ResumeExchange` (variant 18). Accounts: `[exchange, globalstate]`.
pub fn resume_exchange(
    program_id: &Pubkey,
    payer: &Pubkey,
    exchange: &Pubkey,
    args: ExchangeResumeArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::ResumeExchange(args),
        vec![
            AccountMeta::new(*exchange, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `DeleteExchange` (variant 19). Accounts: `[exchange, globalstate]`.
pub fn delete_exchange(
    program_id: &Pubkey,
    payer: &Pubkey,
    exchange: &Pubkey,
    args: ExchangeDeleteArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::DeleteExchange(args),
        vec![
            AccountMeta::new(*exchange, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

/// `SetDeviceExchange` (variant 65). Accounts: `[exchange, device, globalstate]`.
///
/// `device` is on the side selected by `args.index` (1 or 2); `args.set` chooses
/// set vs remove. On `Remove`, pass the currently-set device (`exchange.deviceN_pk`)
/// — the processor decrements the passed device's `reference_count` without
/// checking it matches the exchange's stored key.
pub fn set_device_exchange(
    program_id: &Pubkey,
    payer: &Pubkey,
    exchange: &Pubkey,
    device: &Pubkey,
    args: ExchangeSetDeviceArgs,
) -> Instruction {
    let (globalstate, _) = get_globalstate_pda(program_id);
    common::build_with_permission(
        program_id,
        DoubleZeroInstruction::SetDeviceExchange(args),
        vec![
            AccountMeta::new(*exchange, false),
            AccountMeta::new(*device, false),
            AccountMeta::new(globalstate, false),
        ],
        payer,
    )
}

#[cfg(test)]
mod tests {
    use super::*;
    use solana_system_interface::program as system_program;

    fn create_args() -> ExchangeCreateArgs {
        ExchangeCreateArgs {
            code: "xch".to_string(),
            name: "Exchange".to_string(),
            lat: 1.0,
            lng: 2.0,
            reserved: 0,
        }
    }

    #[test]
    fn test_create_and_update_exchange_include_globalconfig() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();
        let (globalconfig, _) = get_globalconfig_pda(&pid);
        let (globalstate, _) = get_globalstate_pda(&pid);

        let create = create_exchange(&pid, &payer, 1, create_args());
        assert_eq!(create.data[0], 15);
        let (exchange_pda, _) = get_exchange_pda(&pid, 1);
        assert_eq!(
            create.accounts,
            vec![
                AccountMeta::new(exchange_pda, false),
                AccountMeta::new(globalconfig, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );

        let update = update_exchange(&pid, &payer, &exchange, ExchangeUpdateArgs::default());
        assert_eq!(update.data[0], 16);
        assert_eq!(
            update.accounts,
            vec![
                AccountMeta::new(exchange, false),
                AccountMeta::new(globalconfig, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }

    #[test]
    fn test_exchange_lifecycle_verbs() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();
        let (globalstate, _) = get_globalstate_pda(&pid);
        let expected = vec![
            AccountMeta::new(exchange, false),
            AccountMeta::new(globalstate, false),
            AccountMeta::new(payer, true),
            AccountMeta::new(system_program::ID, false),
        ];
        for (ix, tag) in [
            (
                suspend_exchange(&pid, &payer, &exchange, ExchangeSuspendArgs {}),
                17,
            ),
            (
                resume_exchange(&pid, &payer, &exchange, ExchangeResumeArgs {}),
                18,
            ),
            (
                delete_exchange(&pid, &payer, &exchange, ExchangeDeleteArgs {}),
                19,
            ),
        ] {
            assert_eq!(ix.data[0], tag);
            assert_eq!(ix.accounts, expected);
        }
    }

    #[test]
    fn test_set_device_exchange() {
        let pid = Pubkey::new_unique();
        let payer = Pubkey::new_unique();
        let exchange = Pubkey::new_unique();
        let device = Pubkey::new_unique();
        let ix = set_device_exchange(
            &pid,
            &payer,
            &exchange,
            &device,
            ExchangeSetDeviceArgs::default(),
        );
        assert_eq!(ix.data[0], 65);
        let (globalstate, _) = get_globalstate_pda(&pid);
        assert_eq!(
            ix.accounts,
            vec![
                AccountMeta::new(exchange, false),
                AccountMeta::new(device, false),
                AccountMeta::new(globalstate, false),
                AccountMeta::new(payer, true),
                AccountMeta::new(system_program::ID, false),
            ]
        );
    }
}
