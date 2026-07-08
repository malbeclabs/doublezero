use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};
use spl_associated_token_account_interface::address::get_associated_token_address;

use crate::shred_subscription::state;

/// Accounts for the `InitializeClientSeat` instruction (6 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeClientSeatAccounts {
    pub program_config_key: Pubkey,
    pub execution_controller_key: Pubkey,
    pub device_history_key: Pubkey,
    pub payer_key: Pubkey,
    pub new_client_seat_key: Pubkey,
}

impl InitializeClientSeatAccounts {
    pub fn new(payer: &Pubkey, device_key: &Pubkey, client_ip_bits: u32) -> Self {
        Self {
            program_config_key: state::find_program_config_address().0,
            execution_controller_key: state::find_execution_controller_address().0,
            device_history_key: state::find_device_history_address(device_key).0,
            payer_key: *payer,
            new_client_seat_key: state::find_client_seat_address(device_key, client_ip_bits).0,
        }
    }
}

impl From<InitializeClientSeatAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeClientSeatAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new(accounts.execution_controller_key, false),
            AccountMeta::new_readonly(accounts.device_history_key, false),
            AccountMeta::new(accounts.payer_key, true),
            AccountMeta::new(accounts.new_client_seat_key, false),
            AccountMeta::new_readonly(solana_sdk_ids::system_program::ID, false),
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
            AccountMeta::new(accounts.client_seat_key, false),
            AccountMeta::new(accounts.withdraw_authority_key, true),
            AccountMeta::new(accounts.new_payment_escrow_key, false),
            AccountMeta::new_readonly(solana_sdk_ids::system_program::ID, false),
        ]
    }
}

/// Accounts for the `ClosePaymentEscrow` instruction (9 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClosePaymentEscrowAccounts {
    pub program_config_key: Pubkey,
    pub execution_controller_key: Pubkey,
    pub payment_escrow_key: Pubkey,
    pub withdraw_authority_key: Pubkey,
    pub client_seat_key: Pubkey,
    pub device_history_key: Pubkey,
    pub device_history_usdc_token_account_key: Pubkey,
    pub refund_usdc_token_account_key: Pubkey,
}

impl ClosePaymentEscrowAccounts {
    pub fn new(
        device_key: &Pubkey,
        client_ip_bits: u32,
        withdraw_authority: &Pubkey,
        usdc_mint: &Pubkey,
        refund_usdc_token_account: Option<&Pubkey>,
    ) -> Self {
        let refund_key = refund_usdc_token_account
            .copied()
            .unwrap_or_else(|| get_associated_token_address(withdraw_authority, usdc_mint));
        let client_seat_key = state::find_client_seat_address(device_key, client_ip_bits).0;
        let device_history_key = state::find_device_history_address(device_key).0;
        Self {
            program_config_key: state::find_program_config_address().0,
            execution_controller_key: state::find_execution_controller_address().0,
            payment_escrow_key: state::find_payment_escrow_address(
                &client_seat_key,
                withdraw_authority,
            )
            .0,
            withdraw_authority_key: *withdraw_authority,
            client_seat_key,
            device_history_key,
            device_history_usdc_token_account_key: state::find_token_pda_address(
                &device_history_key,
                usdc_mint,
            )
            .0,
            refund_usdc_token_account_key: refund_key,
        }
    }
}

impl From<ClosePaymentEscrowAccounts> for Vec<AccountMeta> {
    fn from(accounts: ClosePaymentEscrowAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new_readonly(accounts.execution_controller_key, false),
            AccountMeta::new(accounts.payment_escrow_key, false),
            AccountMeta::new(accounts.withdraw_authority_key, true),
            AccountMeta::new(accounts.client_seat_key, false),
            AccountMeta::new_readonly(accounts.device_history_key, false),
            AccountMeta::new(accounts.device_history_usdc_token_account_key, false),
            AccountMeta::new(accounts.refund_usdc_token_account_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ]
    }
}

/// Accounts for the `RequestInstantSeatAllocation` instruction (9 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RequestInstantSeatAllocationAccounts {
    pub program_config_key: Pubkey,
    pub execution_controller_key: Pubkey,
    pub metro_history_key: Pubkey,
    pub device_history_key: Pubkey,
    pub client_seat_key: Pubkey,
    pub payment_escrow_key: Pubkey,
    pub payer_key: Pubkey,
    pub new_instant_allocation_request_key: Pubkey,
}

impl RequestInstantSeatAllocationAccounts {
    pub fn new(
        exchange_key: &Pubkey,
        device_key: &Pubkey,
        client_ip_bits: u32,
        withdraw_authority_key: &Pubkey,
        payer_key: &Pubkey,
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
            payer_key: *payer_key,
            new_instant_allocation_request_key: state::find_instant_allocation_request_address(
                device_key,
                client_ip_bits,
            )
            .0,
        }
    }
}

impl From<RequestInstantSeatAllocationAccounts> for Vec<AccountMeta> {
    fn from(accounts: RequestInstantSeatAllocationAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new(accounts.execution_controller_key, false),
            AccountMeta::new_readonly(accounts.metro_history_key, false),
            AccountMeta::new(accounts.device_history_key, false),
            AccountMeta::new(accounts.client_seat_key, false),
            AccountMeta::new(accounts.payment_escrow_key, false),
            AccountMeta::new(accounts.payer_key, true),
            AccountMeta::new(accounts.new_instant_allocation_request_key, false),
            AccountMeta::new_readonly(solana_sdk_ids::system_program::ID, false),
        ]
    }
}

/// Accounts for the `RequestInstantSeatWithdrawal` instruction (7 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RequestInstantSeatWithdrawalAccounts {
    pub program_config_key: Pubkey,
    pub execution_controller_key: Pubkey,
    pub client_seat_key: Pubkey,
    pub device_history_key: Pubkey,
    pub payer_key: Pubkey,
    pub withdraw_seat_request_key: Pubkey,
}

impl RequestInstantSeatWithdrawalAccounts {
    pub fn new(device_key: &Pubkey, client_ip_bits: u32, payer_key: &Pubkey) -> Self {
        let client_seat_key = state::find_client_seat_address(device_key, client_ip_bits).0;
        Self {
            program_config_key: state::find_program_config_address().0,
            execution_controller_key: state::find_execution_controller_address().0,
            client_seat_key,
            device_history_key: state::find_device_history_address(device_key).0,
            payer_key: *payer_key,
            withdraw_seat_request_key: state::find_withdraw_seat_request_address(&client_seat_key)
                .0,
        }
    }
}

impl From<RequestInstantSeatWithdrawalAccounts> for Vec<AccountMeta> {
    fn from(accounts: RequestInstantSeatWithdrawalAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new(accounts.execution_controller_key, false),
            AccountMeta::new(accounts.client_seat_key, false),
            AccountMeta::new(accounts.device_history_key, false),
            AccountMeta::new(accounts.payer_key, true),
            AccountMeta::new(accounts.withdraw_seat_request_key, false),
            AccountMeta::new_readonly(solana_sdk_ids::system_program::ID, false),
        ]
    }
}

/// Accounts for the `RequestProratedInstantSeatWithdrawal` instruction (12 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct RequestProratedInstantSeatWithdrawalAccounts {
    pub program_config_key: Pubkey,
    pub execution_controller_key: Pubkey,
    pub client_seat_key: Pubkey,
    pub device_history_key: Pubkey,
    pub payment_escrow_key: Pubkey,
    pub shred_distribution_key: Pubkey,
    pub device_history_usdc_token_account_key: Pubkey,
    pub shred_distribution_usdc_ata_key: Pubkey,
    pub payer_key: Pubkey,
    pub withdraw_seat_request_key: Pubkey,
}

impl RequestProratedInstantSeatWithdrawalAccounts {
    pub fn new(
        device_key: &Pubkey,
        client_ip_bits: u32,
        funding_authority_key: &Pubkey,
        subscription_epoch: u64,
        usdc_mint_key: &Pubkey,
        payer_key: &Pubkey,
    ) -> Self {
        let client_seat_key = state::find_client_seat_address(device_key, client_ip_bits).0;
        let device_history_key = state::find_device_history_address(device_key).0;
        let shred_distribution_key = state::find_shred_distribution_address(subscription_epoch).0;
        Self {
            program_config_key: state::find_program_config_address().0,
            execution_controller_key: state::find_execution_controller_address().0,
            client_seat_key,
            device_history_key,
            payment_escrow_key: state::find_payment_escrow_address(
                &client_seat_key,
                funding_authority_key,
            )
            .0,
            shred_distribution_key,
            device_history_usdc_token_account_key: state::find_token_pda_address(
                &device_history_key,
                usdc_mint_key,
            )
            .0,
            shred_distribution_usdc_ata_key: get_associated_token_address(
                &shred_distribution_key,
                usdc_mint_key,
            ),
            payer_key: *payer_key,
            withdraw_seat_request_key: state::find_withdraw_seat_request_address(&client_seat_key)
                .0,
        }
    }
}

impl From<RequestProratedInstantSeatWithdrawalAccounts> for Vec<AccountMeta> {
    fn from(accounts: RequestProratedInstantSeatWithdrawalAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new(accounts.execution_controller_key, false),
            AccountMeta::new(accounts.client_seat_key, false),
            AccountMeta::new(accounts.device_history_key, false),
            AccountMeta::new(accounts.payment_escrow_key, false),
            AccountMeta::new(accounts.shred_distribution_key, false),
            AccountMeta::new(accounts.device_history_usdc_token_account_key, false),
            AccountMeta::new(accounts.shred_distribution_usdc_ata_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new(accounts.payer_key, true),
            AccountMeta::new(accounts.withdraw_seat_request_key, false),
            AccountMeta::new_readonly(solana_sdk_ids::system_program::ID, false),
        ]
    }
}

/// Accounts for the `SetValidatorClientRewardsProportion` instruction (3 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SetValidatorClientRewardsProportionAccounts {
    pub program_config_key: Pubkey,
    pub manager_key: Pubkey,
    pub validator_client_rewards_key: Pubkey,
}

impl SetValidatorClientRewardsProportionAccounts {
    pub fn new(manager_key: &Pubkey, client_id: u16) -> Self {
        Self {
            program_config_key: state::find_program_config_address().0,
            manager_key: *manager_key,
            validator_client_rewards_key: state::find_validator_client_rewards_address(client_id).0,
        }
    }
}

impl From<SetValidatorClientRewardsProportionAccounts> for Vec<AccountMeta> {
    fn from(accounts: SetValidatorClientRewardsProportionAccounts) -> Self {
        vec![
            AccountMeta::new(accounts.program_config_key, false),
            AccountMeta::new_readonly(accounts.manager_key, true),
            AccountMeta::new_readonly(accounts.validator_client_rewards_key, false),
        ]
    }
}

/// Accounts for the `CheckCliVersion` instruction (1 account).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct CheckCliVersionAccounts {
    pub program_config_key: Pubkey,
}

impl Default for CheckCliVersionAccounts {
    fn default() -> Self {
        Self::new()
    }
}

impl CheckCliVersionAccounts {
    pub fn new() -> Self {
        Self {
            program_config_key: state::find_program_config_address().0,
        }
    }
}

impl From<CheckCliVersionAccounts> for Vec<AccountMeta> {
    fn from(accounts: CheckCliVersionAccounts) -> Self {
        vec![AccountMeta::new_readonly(
            accounts.program_config_key,
            false,
        )]
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
    pub device_history_usdc_token_account_key: Pubkey,
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
        let device_history_key = state::find_device_history_address(device_key).0;
        Self {
            program_config_key: state::find_program_config_address().0,
            execution_controller_key: state::find_execution_controller_address().0,
            metro_history_key: state::find_metro_history_address(exchange_key).0,
            device_history_key,
            client_seat_key,
            payment_escrow_key: state::find_payment_escrow_address(
                &client_seat_key,
                withdraw_authority_key,
            )
            .0,
            device_history_usdc_token_account_key: state::find_token_pda_address(
                &device_history_key,
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
            AccountMeta::new(accounts.device_history_usdc_token_account_key, false),
            AccountMeta::new(accounts.source_usdc_token_account_key, false),
            AccountMeta::new_readonly(accounts.transfer_authority_key, true),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ]
    }
}

/// Accounts for the `InitializeClaimHolding` instruction (6 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeClaimHoldingAccounts {
    pub parent_pda_key: Pubkey,
    pub payer_key: Pubkey,
    pub new_claim_holding_key: Pubkey,
    pub mint_key: Pubkey,
}

impl InitializeClaimHoldingAccounts {
    pub fn new(
        client_id: u16,
        subscription_epoch: u64,
        mint_key: &Pubkey,
        payer_key: &Pubkey,
    ) -> Self {
        let parent_pda_key = state::find_validator_client_rewards_address(client_id).0;
        let new_claim_holding_key =
            state::find_claim_holding_address(&parent_pda_key, subscription_epoch, mint_key).0;
        Self {
            parent_pda_key,
            payer_key: *payer_key,
            new_claim_holding_key,
            mint_key: *mint_key,
        }
    }
}

impl From<InitializeClaimHoldingAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeClaimHoldingAccounts) -> Self {
        vec![
            AccountMeta::new(accounts.parent_pda_key, false),
            AccountMeta::new(accounts.payer_key, true),
            AccountMeta::new(accounts.new_claim_holding_key, false),
            AccountMeta::new_readonly(accounts.mint_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
            AccountMeta::new_readonly(solana_sdk_ids::system_program::ID, false),
        ]
    }
}

/// Accounts for the `ClaimValidatorClientRewards` instruction (6 fixed +
/// one writable per claim holding in `claim_holding_account_keys`).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClaimValidatorClientRewardsAccounts {
    pub program_config_key: Pubkey,
    pub validator_client_rewards_key: Pubkey,
    pub manager_key: Pubkey,
    pub destination_token_account_key: Pubkey,
    pub rent_beneficiary_key: Pubkey,
    pub claim_holding_account_keys: Vec<Pubkey>,
}

impl ClaimValidatorClientRewardsAccounts {
    pub fn new(
        client_id: u16,
        manager_key: &Pubkey,
        destination_token_account_key: &Pubkey,
        rent_beneficiary_key: &Pubkey,
        mint_key: &Pubkey,
        subscription_epochs: &[u64],
    ) -> Self {
        let validator_client_rewards_key =
            state::find_validator_client_rewards_address(client_id).0;
        let claim_holding_account_keys = subscription_epochs
            .iter()
            .map(|epoch| {
                state::find_claim_holding_address(&validator_client_rewards_key, *epoch, mint_key).0
            })
            .collect();
        Self {
            program_config_key: state::find_program_config_address().0,
            validator_client_rewards_key,
            manager_key: *manager_key,
            destination_token_account_key: *destination_token_account_key,
            rent_beneficiary_key: *rent_beneficiary_key,
            claim_holding_account_keys,
        }
    }
}

impl From<ClaimValidatorClientRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: ClaimValidatorClientRewardsAccounts) -> Self {
        let mut metas = vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new(accounts.validator_client_rewards_key, false),
            AccountMeta::new_readonly(accounts.manager_key, true),
            AccountMeta::new(accounts.destination_token_account_key, false),
            AccountMeta::new(accounts.rent_beneficiary_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ];
        metas.extend(
            accounts
                .claim_holding_account_keys
                .into_iter()
                .map(|key| AccountMeta::new(key, false)),
        );
        metas
    }
}

/// Accounts for the `InitializeValidatorPublisherRewards` instruction.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InitializeValidatorPublisherRewardsAccounts {
    pub payer_key: Pubkey,
    pub new_validator_publisher_rewards_key: Pubkey,
}

impl InitializeValidatorPublisherRewardsAccounts {
    pub fn new(payer_key: &Pubkey, node_id: &Pubkey) -> Self {
        Self {
            payer_key: *payer_key,
            new_validator_publisher_rewards_key: state::find_validator_publisher_rewards_address(
                node_id,
            )
            .0,
        }
    }
}

impl From<InitializeValidatorPublisherRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: InitializeValidatorPublisherRewardsAccounts) -> Self {
        vec![
            AccountMeta::new(accounts.payer_key, true),
            AccountMeta::new(accounts.new_validator_publisher_rewards_key, false),
            AccountMeta::new_readonly(solana_sdk_ids::system_program::ID, false),
        ]
    }
}

/// Accounts for the `ConfigureValidatorPublisherRewards` instruction.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConfigureValidatorPublisherRewardsAccounts {
    pub program_config_key: Pubkey,
    pub shred_reward_token_key: Pubkey,
    pub validator_node_key: Pubkey,
    pub validator_publisher_rewards_key: Pubkey,
    /// `true` when the `validator_node` account is a Solana signer on the
    /// transaction (direct path); `false` when authorization is carried in
    /// instruction data via `ValidatorOffchainAuthorization`.
    pub is_node_signer: bool,
}

impl ConfigureValidatorPublisherRewardsAccounts {
    pub fn new(node_id: &Pubkey, rewards_token_mint_key: &Pubkey, is_node_signer: bool) -> Self {
        Self {
            program_config_key: state::find_program_config_address().0,
            shred_reward_token_key: state::find_shred_reward_token_address(rewards_token_mint_key)
                .0,
            validator_node_key: *node_id,
            validator_publisher_rewards_key: state::find_validator_publisher_rewards_address(
                node_id,
            )
            .0,
            is_node_signer,
        }
    }
}

impl From<ConfigureValidatorPublisherRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: ConfigureValidatorPublisherRewardsAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new_readonly(accounts.shred_reward_token_key, false),
            AccountMeta::new_readonly(accounts.validator_node_key, accounts.is_node_signer),
            AccountMeta::new(accounts.validator_publisher_rewards_key, false),
        ]
    }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct DistributeValidatorRewardsAccounts {
    pub program_config_key: Pubkey,
    pub shred_distribution_key: Pubkey,
    pub parent_distribution_key: Pubkey,
    pub validator_publisher_rewards_key: Pubkey,
    pub validator_client_rewards_key: Pubkey,
    pub validator_publisher_journal_key: Pubkey,
    // Omitted when the publisher journal IS the client journal (omit-rule
    // fires when `client_mint_key` equals `publisher_mint_key`). The publisher
    // journal then plays both roles, mirroring accumulate.
    pub validator_client_journal_key: Option<Pubkey>,
    pub destination_ata_key: Pubkey,
    pub shred_distribution_publisher_ata_key: Pubkey,
    pub shred_distribution_client_ata_key: Pubkey,
    pub client_claim_holding_key: Pubkey,
}

#[derive(Debug)]
pub struct DistributeValidatorRewardsAccountsInitializer<'a> {
    pub subscription_epoch: u64,
    pub associated_dz_epoch: u64,
    pub node_id: &'a Pubkey,
    pub client_id: u16,
    pub rewards_token_owner_key: &'a Pubkey,
    pub publisher_mint_key: &'a Pubkey,
    pub publisher_reward_mint_key: &'a Pubkey,
    /// Mint that identifies the client-side journal. In the current
    /// protocol this is always the 2Z mint (client rewards are routed
    /// exclusively to the 2Z journal), so every caller today passes the
    /// 2Z mint — but the field is a generic `&Pubkey` so a future
    /// protocol version that routes client rewards to a different mint
    /// works without an API change.
    pub client_mint_key: &'a Pubkey,
}

impl DistributeValidatorRewardsAccounts {
    pub fn new(initializer: DistributeValidatorRewardsAccountsInitializer<'_>) -> Self {
        let DistributeValidatorRewardsAccountsInitializer {
            subscription_epoch,
            associated_dz_epoch,
            node_id,
            client_id,
            rewards_token_owner_key,
            publisher_mint_key,
            publisher_reward_mint_key,
            client_mint_key,
        } = initializer;

        let shred_distribution_key = state::find_shred_distribution_address(subscription_epoch).0;
        let validator_client_rewards_key =
            state::find_validator_client_rewards_address(client_id).0;

        // Omit-rule: when the publisher journal IS the client journal
        // (their mints match), the publisher journal plays both roles and
        // the client-side journal account drops out of the meta list.
        // Otherwise the client side has its own journal at the 2Z mint,
        // and the client-side ATA / claim_holding use the 2Z mint too.
        let client_side_present = client_mint_key != publisher_mint_key;
        let validator_client_journal_key = client_side_present.then(|| {
            state::find_shred_distribution_journal_address(subscription_epoch, client_mint_key).0
        });
        let client_addresses_mint_key = if client_side_present {
            client_mint_key
        } else {
            publisher_reward_mint_key
        };

        Self {
            program_config_key: state::find_program_config_address().0,
            shred_distribution_key,
            parent_distribution_key:
                crate::revenue_distribution::state::Distribution::find_address(
                    crate::revenue_distribution::types::DoubleZeroEpoch::new(associated_dz_epoch),
                )
                .0,
            validator_publisher_rewards_key: state::find_validator_publisher_rewards_address(
                node_id,
            )
            .0,
            validator_client_rewards_key,
            validator_publisher_journal_key: state::find_shred_distribution_journal_address(
                subscription_epoch,
                publisher_mint_key,
            )
            .0,
            validator_client_journal_key,
            destination_ata_key: get_associated_token_address(
                rewards_token_owner_key,
                publisher_reward_mint_key,
            ),
            shred_distribution_publisher_ata_key: get_associated_token_address(
                &shred_distribution_key,
                publisher_reward_mint_key,
            ),
            shred_distribution_client_ata_key: get_associated_token_address(
                &shred_distribution_key,
                client_addresses_mint_key,
            ),
            client_claim_holding_key: state::find_claim_holding_address(
                &validator_client_rewards_key,
                subscription_epoch,
                client_addresses_mint_key,
            )
            .0,
        }
    }
}

impl From<DistributeValidatorRewardsAccountsInitializer<'_>>
    for DistributeValidatorRewardsAccounts
{
    fn from(initializer: DistributeValidatorRewardsAccountsInitializer<'_>) -> Self {
        Self::new(initializer)
    }
}

impl From<DistributeValidatorRewardsAccountsInitializer<'_>> for Vec<AccountMeta> {
    fn from(initializer: DistributeValidatorRewardsAccountsInitializer<'_>) -> Self {
        DistributeValidatorRewardsAccounts::new(initializer).into()
    }
}

impl From<DistributeValidatorRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: DistributeValidatorRewardsAccounts) -> Self {
        let DistributeValidatorRewardsAccounts {
            program_config_key,
            shred_distribution_key,
            parent_distribution_key,
            validator_publisher_rewards_key,
            validator_client_rewards_key,
            validator_publisher_journal_key,
            validator_client_journal_key,
            destination_ata_key,
            shred_distribution_publisher_ata_key,
            shred_distribution_client_ata_key,
            client_claim_holding_key,
        } = accounts;

        let mut account_metas = vec![
            AccountMeta::new_readonly(program_config_key, false),
            AccountMeta::new_readonly(shred_distribution_key, false),
            AccountMeta::new_readonly(parent_distribution_key, false),
            AccountMeta::new_readonly(validator_publisher_rewards_key, false),
            AccountMeta::new_readonly(validator_client_rewards_key, false),
            AccountMeta::new(validator_publisher_journal_key, false),
        ];

        if let Some(validator_client_journal_key) = validator_client_journal_key {
            account_metas.push(AccountMeta::new(validator_client_journal_key, false));
        }

        account_metas.extend([
            AccountMeta::new(destination_ata_key, false),
            AccountMeta::new(shred_distribution_publisher_ata_key, false),
            AccountMeta::new(shred_distribution_client_ata_key, false),
            AccountMeta::new(client_claim_holding_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ]);

        account_metas
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn initialize_claim_holding_metas_order() {
        let payer = Pubkey::new_unique();
        let mint = Pubkey::new_unique();
        let client_id: u16 = 7;
        let epoch: u64 = 1234;
        let accounts = InitializeClaimHoldingAccounts::new(client_id, epoch, &mint, &payer);
        let parent_pda = state::find_validator_client_rewards_address(client_id).0;
        let holding_pda = state::find_claim_holding_address(&parent_pda, epoch, &mint).0;
        assert_eq!(accounts.parent_pda_key, parent_pda);
        assert_eq!(accounts.new_claim_holding_key, holding_pda);
        assert_eq!(accounts.payer_key, payer);
        assert_eq!(accounts.mint_key, mint);

        let metas: Vec<AccountMeta> = accounts.into();
        assert_eq!(metas.len(), 6);
        // 0: parent_pda (writable, not signer)
        assert_eq!(metas[0].pubkey, parent_pda);
        assert!(metas[0].is_writable && !metas[0].is_signer);
        // 1: payer (writable, signer)
        assert_eq!(metas[1].pubkey, payer);
        assert!(metas[1].is_writable && metas[1].is_signer);
        // 2: new_holding (writable, not signer)
        assert_eq!(metas[2].pubkey, holding_pda);
        assert!(metas[2].is_writable && !metas[2].is_signer);
        // 3: mint (readonly, not signer)
        assert_eq!(metas[3].pubkey, mint);
        assert!(!metas[3].is_writable && !metas[3].is_signer);
        // 4: spl token program (readonly, not signer)
        assert_eq!(metas[4].pubkey, spl_token_interface::ID);
        assert!(!metas[4].is_writable && !metas[4].is_signer);
        // 5: system program (readonly, not signer)
        assert_eq!(metas[5].pubkey, solana_sdk_ids::system_program::ID);
        assert!(!metas[5].is_writable && !metas[5].is_signer);
    }

    #[test]
    fn claim_validator_client_rewards_metas_order_empty() {
        let manager = Pubkey::new_unique();
        let destination = Pubkey::new_unique();
        let rent_beneficiary = Pubkey::new_unique();
        let mint = Pubkey::new_unique();
        let client_id: u16 = 11;
        let accounts = ClaimValidatorClientRewardsAccounts::new(
            client_id,
            &manager,
            &destination,
            &rent_beneficiary,
            &mint,
            &[],
        );
        let vcr = state::find_validator_client_rewards_address(client_id).0;
        let cfg = state::find_program_config_address().0;
        assert_eq!(accounts.program_config_key, cfg);
        assert_eq!(accounts.validator_client_rewards_key, vcr);
        assert_eq!(accounts.manager_key, manager);
        assert_eq!(accounts.destination_token_account_key, destination);
        assert_eq!(accounts.rent_beneficiary_key, rent_beneficiary);
        assert!(accounts.claim_holding_account_keys.is_empty());

        let metas: Vec<AccountMeta> = accounts.into();
        assert_eq!(metas.len(), 6);
        // 0: program_config (readonly, not signer)
        assert_eq!(metas[0].pubkey, cfg);
        assert!(!metas[0].is_writable && !metas[0].is_signer);
        // 1: VCR (writable, not signer)
        assert_eq!(metas[1].pubkey, vcr);
        assert!(metas[1].is_writable && !metas[1].is_signer);
        // 2: manager (readonly, SIGNER)
        assert_eq!(metas[2].pubkey, manager);
        assert!(!metas[2].is_writable && metas[2].is_signer);
        // 3: destination (writable, not signer)
        assert_eq!(metas[3].pubkey, destination);
        assert!(metas[3].is_writable && !metas[3].is_signer);
        // 4: rent_beneficiary (writable, not signer)
        assert_eq!(metas[4].pubkey, rent_beneficiary);
        assert!(metas[4].is_writable && !metas[4].is_signer);
        // 5: spl token program (readonly, not signer)
        assert_eq!(metas[5].pubkey, spl_token_interface::ID);
        assert!(!metas[5].is_writable && !metas[5].is_signer);
    }

    #[test]
    fn claim_validator_client_rewards_metas_with_three_holdings() {
        let manager = Pubkey::new_unique();
        let destination = Pubkey::new_unique();
        let rent_beneficiary = Pubkey::new_unique();
        let mint = Pubkey::new_unique();
        let client_id: u16 = 11;
        let epochs: &[u64] = &[100, 101, 102];
        let accounts = ClaimValidatorClientRewardsAccounts::new(
            client_id,
            &manager,
            &destination,
            &rent_beneficiary,
            &mint,
            epochs,
        );
        let vcr = state::find_validator_client_rewards_address(client_id).0;
        let expected_holdings: Vec<Pubkey> = epochs
            .iter()
            .map(|e| state::find_claim_holding_address(&vcr, *e, &mint).0)
            .collect();
        assert_eq!(accounts.claim_holding_account_keys, expected_holdings);

        let metas: Vec<AccountMeta> = accounts.into();
        assert_eq!(metas.len(), 6 + 3);
        for (i, expected_holding) in expected_holdings.iter().enumerate() {
            let meta = &metas[6 + i];
            assert_eq!(meta.pubkey, *expected_holding);
            assert!(meta.is_writable && !meta.is_signer);
        }
    }

    #[test]
    fn initialize_vpr_account_metas() {
        let payer = Pubkey::new_unique();
        let node_id = Pubkey::new_unique();
        let metas: Vec<AccountMeta> =
            InitializeValidatorPublisherRewardsAccounts::new(&payer, &node_id).into();
        assert_eq!(metas.len(), 3);
        // 0: payer (signer, mut)
        assert!(metas[0].is_signer);
        assert!(metas[0].is_writable);
        assert_eq!(metas[0].pubkey, payer);
        // 1: new VPR PDA (mut, not signer)
        assert!(!metas[1].is_signer);
        assert!(metas[1].is_writable);
        // 2: system program (ro)
        assert!(!metas[2].is_signer);
        assert!(!metas[2].is_writable);
        assert_eq!(metas[2].pubkey, solana_sdk_ids::system_program::ID);
    }

    #[test]
    fn configure_vpr_account_metas_direct() {
        let node_id = Pubkey::new_unique();
        let mint = Pubkey::new_unique();
        let metas: Vec<AccountMeta> = ConfigureValidatorPublisherRewardsAccounts::new(
            &node_id, &mint, /* is_node_signer = */ true,
        )
        .into();
        assert_eq!(metas.len(), 4);
        // 0: program_config (ro)
        assert!(!metas[0].is_signer);
        assert!(!metas[0].is_writable);
        // 1: shred_reward_token (ro)
        assert!(!metas[1].is_signer);
        assert!(!metas[1].is_writable);
        // 2: validator_node (signer in direct path, ro)
        assert!(metas[2].is_signer);
        assert!(!metas[2].is_writable);
        assert_eq!(metas[2].pubkey, node_id);
        // 3: vpr PDA (mut)
        assert!(!metas[3].is_signer);
        assert!(metas[3].is_writable);
    }

    #[test]
    fn configure_vpr_account_metas_offchain() {
        let node_id = Pubkey::new_unique();
        let mint = Pubkey::new_unique();
        let metas: Vec<AccountMeta> = ConfigureValidatorPublisherRewardsAccounts::new(
            &node_id, &mint, /* is_node_signer = */ false,
        )
        .into();
        // Validator node not a signer in offchain path.
        assert!(!metas[2].is_signer);
    }
}
