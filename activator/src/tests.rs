#[cfg(test)]
pub mod utils {
    use doublezero_sdk::{AccountData, AccountType, DoubleZeroClient, MockDoubleZeroClient};
    use doublezero_serviceability::{
        pda::{get_device_pda, get_globalstate_pda, get_link_pda, get_user_old_pda},
        state::globalstate::GlobalState,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    pub fn create_test_client() -> MockDoubleZeroClient {
        let mut client = MockDoubleZeroClient::new();

        let payer = Pubkey::new_unique();

        // Program ID
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        // Global State
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 0,
            account_index: 0,
            foundation_allowlist: vec![payer],
            device_allowlist: vec![payer],
            user_allowlist: vec![payer],
            activator_authority_pk: payer,
            sentinel_authority_pk: payer,
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
            health_oracle_pk: payer,
        };

        client.expect_get_payer().returning(move || payer);

        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        client
    }

    pub fn get_device_bump_seed(client: &MockDoubleZeroClient) -> u8 {
        let (_, bump_seed) = get_device_pda(&client.get_program_id(), 0);
        bump_seed
    }

    pub fn get_tunnel_bump_seed(client: &MockDoubleZeroClient) -> u8 {
        let (_, bump_seed) = get_link_pda(&client.get_program_id(), 0);
        bump_seed
    }

    pub fn get_user_bump_seed(client: &MockDoubleZeroClient) -> u8 {
        let (_, bump_seed) = get_user_old_pda(&client.get_program_id(), 0);
        bump_seed
    }
}
