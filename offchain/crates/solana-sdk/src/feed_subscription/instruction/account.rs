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

/// Accounts for the `ConfigureFeedPublisherRewards` instruction (6 accounts).
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ConfigureFeedPublisherRewardsAccounts {
    pub program_config_key: Pubkey,
    pub feed_publishers_reward_token_key: Pubkey,
    pub validator_node_key: Pubkey,
    pub feed_publisher_rewards_key: Pubkey,
    pub payer_key: Pubkey,
    /// False when the instruction data carries an offchain authorization instead.
    pub is_node_signer: bool,
}

impl ConfigureFeedPublisherRewardsAccounts {
    pub fn new(
        node_id: &Pubkey,
        rewards_token_mint_key: &Pubkey,
        payer_key: &Pubkey,
        is_node_signer: bool,
    ) -> Self {
        Self {
            program_config_key: state::find_program_config_address().0,
            feed_publishers_reward_token_key: state::find_feed_publishers_reward_token_address(
                rewards_token_mint_key,
            )
            .0,
            validator_node_key: *node_id,
            feed_publisher_rewards_key: state::find_feed_publisher_rewards_address(node_id).0,
            payer_key: *payer_key,
            is_node_signer,
        }
    }
}

impl From<ConfigureFeedPublisherRewardsAccounts> for Vec<AccountMeta> {
    fn from(accounts: ConfigureFeedPublisherRewardsAccounts) -> Self {
        vec![
            AccountMeta::new_readonly(accounts.program_config_key, false),
            AccountMeta::new_readonly(accounts.feed_publishers_reward_token_key, false),
            AccountMeta::new_readonly(accounts.validator_node_key, accounts.is_node_signer),
            AccountMeta::new(accounts.feed_publisher_rewards_key, false),
            AccountMeta::new(accounts.payer_key, true),
            AccountMeta::new_readonly(solana_sdk_ids::system_program::ID, false),
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
    fn test_configure_feed_publisher_rewards_account_order_matches_the_program() {
        let node_id = Pubkey::new_from_array([2; 32]);
        let mint_key = Pubkey::new_from_array([4; 32]);
        let payer_key = Pubkey::new_from_array([5; 32]);

        for is_node_signer in [true, false] {
            let metas = Vec::<AccountMeta>::from(ConfigureFeedPublisherRewardsAccounts::new(
                &node_id,
                &mint_key,
                &payer_key,
                is_node_signer,
            ));

            // The program's own accounts struct builds this list for these keys, under the
            // default program ID.
            assert_eq!(
                metas,
                vec![
                    AccountMeta::new_readonly(
                        pubkey!("7qRWt44BRDKzQz3Q85U5fgfRiaSavesCm6zSN33uLEHn"),
                        false
                    ),
                    AccountMeta::new_readonly(
                        pubkey!("ApSE3jmXR2DXa45xfd6ZRGB5rGpLMyWTCydYYaZ2WGdk"),
                        false
                    ),
                    AccountMeta::new_readonly(node_id, is_node_signer),
                    AccountMeta::new(
                        pubkey!("8LHDvyEWFFrAZgqoQbYwRc3yt8LmcaP7jafJMK1EpNk1"),
                        false
                    ),
                    AccountMeta::new(payer_key, true),
                    AccountMeta::new_readonly(solana_sdk_ids::system_program::ID, false),
                ]
            );
        }
    }
}
