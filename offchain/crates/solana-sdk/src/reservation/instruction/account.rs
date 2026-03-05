use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};
use spl_associated_token_account_interface::address::get_associated_token_address;

use crate::reservation::state;

/// Accounts for the `InitializeClientSeat` instruction (9 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeClientSeatAccounts {
    pub program_config_key: Pubkey,
    pub execution_controller_key: Pubkey,
    pub device_history_key: Pubkey,
    pub payer_key: Pubkey,
    pub new_client_seat_key: Pubkey,
    pub new_client_seat_usdc_token_pda_key: Pubkey,
    pub usdc_mint_key: Pubkey,
}

impl InitializeClientSeatAccounts {
    pub fn new(
        payer: &Pubkey,
        device_key: &Pubkey,
        client_ip_bits: u32,
        usdc_mint: &Pubkey,
    ) -> Self {
        let client_seat_key = state::find_client_seat_address(device_key, client_ip_bits).0;
        Self {
            program_config_key: state::find_program_config_address().0,
            execution_controller_key: state::find_execution_controller_address().0,
            device_history_key: state::find_device_history_address(device_key).0,
            payer_key: *payer,
            new_client_seat_key: client_seat_key,
            new_client_seat_usdc_token_pda_key: state::find_token_pda_address(
                &client_seat_key,
                usdc_mint,
            )
            .0,
            usdc_mint_key: *usdc_mint,
        }
    }
}

impl From<InitializeClientSeatAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeClientSeatAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new_readonly(accounts.execution_controller_key, false),
            AccountMeta::new_readonly(accounts.device_history_key, false),
            AccountMeta::new(accounts.payer_key, true),
            AccountMeta::new(accounts.new_client_seat_key, false),
            AccountMeta::new(accounts.new_client_seat_usdc_token_pda_key, false),
            AccountMeta::new_readonly(accounts.usdc_mint_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new_readonly(solana_sdk::system_program::ID, false),
        ]
    }
}

/// Accounts for the `InitializePaymentEscrow` instruction (5 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializePaymentEscrowAccounts {
    pub program_config_key: Pubkey,
    pub client_seat_key: Pubkey,
    pub withdraw_authority_key: Pubkey,
    pub new_payment_escrow_key: Pubkey,
}

impl InitializePaymentEscrowAccounts {
    pub fn new(client_seat_key: &Pubkey, withdraw_authority: &Pubkey) -> Self {
        Self {
            program_config_key: state::find_program_config_address().0,
            client_seat_key: *client_seat_key,
            withdraw_authority_key: *withdraw_authority,
            new_payment_escrow_key: state::find_payment_escrow_address(
                client_seat_key,
                withdraw_authority,
            )
            .0,
        }
    }
}

impl From<InitializePaymentEscrowAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializePaymentEscrowAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new_readonly(accounts.client_seat_key, false),
            AccountMeta::new(accounts.withdraw_authority_key, true),
            AccountMeta::new(accounts.new_payment_escrow_key, false),
            AccountMeta::new_readonly(solana_sdk::system_program::ID, false),
        ]
    }
}

/// Accounts for the `ClosePaymentEscrow` instruction (8 accounts).
///
/// Always passes all 8 accounts. The on-chain program only reads accounts 4-7
/// when `usdc_balance > 0`; extra accounts are harmless when balance is zero.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClosePaymentEscrowAccounts {
    pub program_config_key: Pubkey,
    pub execution_controller_key: Pubkey,
    pub payment_escrow_key: Pubkey,
    pub withdraw_authority_key: Pubkey,
    pub client_seat_key: Pubkey,
    pub client_seat_usdc_token_pda_key: Pubkey,
    pub refund_usdc_token_account_key: Pubkey,
}

impl ClosePaymentEscrowAccounts {
    pub fn new(
        client_seat_key: &Pubkey,
        withdraw_authority: &Pubkey,
        usdc_mint: &Pubkey,
        refund_usdc_token_account: &Pubkey,
    ) -> Self {
        Self {
            program_config_key: state::find_program_config_address().0,
            execution_controller_key: state::find_execution_controller_address().0,
            payment_escrow_key: state::find_payment_escrow_address(
                client_seat_key,
                withdraw_authority,
            )
            .0,
            withdraw_authority_key: *withdraw_authority,
            client_seat_key: *client_seat_key,
            client_seat_usdc_token_pda_key: state::find_token_pda_address(
                client_seat_key,
                usdc_mint,
            )
            .0,
            refund_usdc_token_account_key: *refund_usdc_token_account,
        }
    }

    /// Convenience: derive the refund destination as the withdraw authority's
    /// USDC ATA.
    pub fn new_with_ata_refund(
        client_seat_key: &Pubkey,
        withdraw_authority: &Pubkey,
        usdc_mint: &Pubkey,
    ) -> Self {
        let refund_ata = get_associated_token_address(withdraw_authority, usdc_mint);
        Self::new(client_seat_key, withdraw_authority, usdc_mint, &refund_ata)
    }
}

impl From<ClosePaymentEscrowAccounts> for Vec<AccountMeta> {
    fn from(accounts: ClosePaymentEscrowAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new_readonly(accounts.execution_controller_key, false),
            AccountMeta::new(accounts.payment_escrow_key, false),
            AccountMeta::new(accounts.withdraw_authority_key, true),
            AccountMeta::new_readonly(accounts.client_seat_key, false),
            AccountMeta::new(accounts.client_seat_usdc_token_pda_key, false),
            AccountMeta::new(accounts.refund_usdc_token_account_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ]
    }
}

/// Accounts for the `FundPaymentEscrowUsdc` instruction (10 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct FundPaymentEscrowUsdcAccounts {
    pub program_config_key: Pubkey,
    pub execution_controller_key: Pubkey,
    pub metro_history_key: Pubkey,
    pub device_history_key: Pubkey,
    pub client_seat_key: Pubkey,
    pub payment_escrow_key: Pubkey,
    pub client_seat_usdc_token_account_key: Pubkey,
    pub source_usdc_token_account_key: Pubkey,
    pub transfer_authority_key: Pubkey,
}

impl FundPaymentEscrowUsdcAccounts {
    pub fn new(
        exchange_key: &Pubkey,
        device_key: &Pubkey,
        client_ip_bits: u32,
        withdraw_authority_key: &Pubkey,
        usdc_mint_key: &Pubkey,
        source_usdc_token_account_key: &Pubkey,
        transfer_authority_key: &Pubkey,
    ) -> Self {
        let client_seat_key = state::find_client_seat_address(device_key, client_ip_bits).0;
        Self {
            program_config_key: state::find_program_config_address().0,
            execution_controller_key: state::find_execution_controller_address().0,
            metro_history_key: state::find_metro_history_address(exchange_key).0,
            device_history_key: state::find_device_history_address(device_key).0,
            client_seat_key,
            payment_escrow_key: state::find_payment_escrow_address(
                &client_seat_key,
                withdraw_authority_key,
            )
            .0,
            client_seat_usdc_token_account_key: state::find_token_pda_address(
                &client_seat_key,
                usdc_mint_key,
            )
            .0,
            source_usdc_token_account_key: *source_usdc_token_account_key,
            transfer_authority_key: *transfer_authority_key,
        }
    }
}

impl From<FundPaymentEscrowUsdcAccounts> for Vec<AccountMeta> {
    fn from(accounts: FundPaymentEscrowUsdcAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new(accounts.execution_controller_key, false),
            AccountMeta::new_readonly(accounts.metro_history_key, false),
            AccountMeta::new_readonly(accounts.device_history_key, false),
            AccountMeta::new(accounts.client_seat_key, false),
            AccountMeta::new(accounts.payment_escrow_key, false),
            AccountMeta::new(accounts.client_seat_usdc_token_account_key, false),
            AccountMeta::new(accounts.source_usdc_token_account_key, false),
            AccountMeta::new_readonly(accounts.transfer_authority_key, true),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ]
    }
}
