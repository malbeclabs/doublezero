use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey};

use crate::feed_subscription::state;

/// Accounts for the `ConfigureValidatorClientRewardsProportion` instruction (3 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConfigureValidatorClientRewardsProportionAccounts {
    pub program_config_key: Pubkey,
    pub manager_key: Pubkey,
    pub validator_client_rewards_key: Pubkey,
}

impl ConfigureValidatorClientRewardsProportionAccounts {
    pub fn new(manager_key: &Pubkey, client_id: u16) -> Self {
        Self {
            program_config_key: state::find_program_config_address().0,
            manager_key: *manager_key,
            validator_client_rewards_key: state::find_validator_client_rewards_address(client_id).0,
        }
    }
}

impl From<ConfigureValidatorClientRewardsProportionAccounts> for Vec<AccountMeta> {
    fn from(accounts: ConfigureValidatorClientRewardsProportionAccounts) -> Self {
        vec![
            AccountMeta::new(accounts.program_config_key, false),
            AccountMeta::new_readonly(accounts.manager_key, true),
            AccountMeta::new_readonly(accounts.validator_client_rewards_key, false),
        ]
    }
}

/// Accounts for the `ClaimFeedValidatorClientRewards` instruction (9 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ClaimFeedValidatorClientRewardsAccounts {
    pub program_config_key: Pubkey,
    pub manager_key: Pubkey,
    pub validator_client_rewards_key: Pubkey,
    pub feed_distribution_key: Pubkey,
    pub feed_distribution_journal_key: Pubkey,
    pub claim_holding_key: Pubkey,
    pub destination_token_account_key: Pubkey,
    pub oracle_key: Pubkey,
}

impl ClaimFeedValidatorClientRewardsAccounts {
    pub fn new(
        manager_key: &Pubkey,
        client_id: u16,
        feed_distribution_key: &Pubkey,
        destination_token_account_key: &Pubkey,
        oracle_key: &Pubkey,
        doublezero_mint_key: &Pubkey,
    ) -> Self {
        let validator_client_rewards_key =
            state::find_validator_client_rewards_address(client_id).0;
        Self {
            program_config_key: state::find_program_config_address().0,
            manager_key: *manager_key,
            validator_client_rewards_key,
            feed_distribution_key: *feed_distribution_key,
            feed_distribution_journal_key: state::find_feed_distribution_journal_address(
                feed_distribution_key,
            )
            .0,
            claim_holding_key: state::find_claim_holding_address(
                &validator_client_rewards_key,
                feed_distribution_key,
                doublezero_mint_key,
            )
            .0,
            destination_token_account_key: *destination_token_account_key,
            oracle_key: *oracle_key,
        }
    }
}

impl From<ClaimFeedValidatorClientRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: ClaimFeedValidatorClientRewardsAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new_readonly(accounts.manager_key, true),
            AccountMeta::new(accounts.validator_client_rewards_key, false),
            AccountMeta::new_readonly(accounts.feed_distribution_key, false),
            AccountMeta::new(accounts.feed_distribution_journal_key, false),
            AccountMeta::new(accounts.claim_holding_key, false),
            AccountMeta::new(accounts.destination_token_account_key, false),
            AccountMeta::new(accounts.oracle_key, false),
            AccountMeta::new_readonly(spl_token_interface::ID, false),
        ]
    }
}

#[cfg(test)]
mod tests {
    use solana_sdk::pubkey;

    use super::*;

    #[test]
    fn test_account_order_and_flags_match_the_onchain_idl() {
        let manager_key = Pubkey::new_unique();
        let metas = Vec::<AccountMeta>::from(
            ConfigureValidatorClientRewardsProportionAccounts::new(&manager_key, 9),
        );

        assert_eq!(metas.len(), 3);
        assert_eq!(metas[0].pubkey, state::find_program_config_address().0);
        assert!(metas[0].is_writable && !metas[0].is_signer);
        assert_eq!(metas[1].pubkey, manager_key);
        assert!(!metas[1].is_writable && metas[1].is_signer);
        assert_eq!(
            metas[2].pubkey,
            state::find_validator_client_rewards_address(9).0
        );
        assert!(!metas[2].is_writable && !metas[2].is_signer);
    }

    #[test]
    fn test_claim_feed_validator_client_rewards_account_order_matches_the_program() {
        let manager_key = Pubkey::new_from_array([2; 32]);
        let feed_distribution_key = Pubkey::new_from_array([3; 32]);
        let destination_token_account_key = Pubkey::new_from_array([4; 32]);
        let oracle_key = Pubkey::new_from_array([5; 32]);
        let mint_key = Pubkey::new_from_array([6; 32]);

        let metas = Vec::<AccountMeta>::from(ClaimFeedValidatorClientRewardsAccounts::new(
            &manager_key,
            9,
            &feed_distribution_key,
            &destination_token_account_key,
            &oracle_key,
            &mint_key,
        ));

        // The program's own accounts struct builds this list for these keys, under the default
        // program ID.
        assert_eq!(
            metas,
            vec![
                AccountMeta::new_readonly(
                    pubkey!("7qRWt44BRDKzQz3Q85U5fgfRiaSavesCm6zSN33uLEHn"),
                    false
                ),
                AccountMeta::new_readonly(manager_key, true),
                AccountMeta::new(
                    pubkey!("EfxfTkUbP3Ra65GiayzYGWofuGTJq6T82RNGzukK81wc"),
                    false
                ),
                AccountMeta::new_readonly(feed_distribution_key, false),
                AccountMeta::new(
                    pubkey!("F5nMTFLLMjzWRJiMJmnPG1MR1WDyjixKzfBh9cGz9TEM"),
                    false
                ),
                AccountMeta::new(
                    pubkey!("C8u7uYbdGgosLE3CLG8j5CDRcm3mVTnaikbdKdj78Gvg"),
                    false
                ),
                AccountMeta::new(destination_token_account_key, false),
                AccountMeta::new(oracle_key, false),
                AccountMeta::new_readonly(spl_token_interface::ID, false),
            ]
        );
    }
}
