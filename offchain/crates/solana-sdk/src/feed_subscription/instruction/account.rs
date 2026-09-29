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

#[cfg(test)]
mod tests {
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
}
