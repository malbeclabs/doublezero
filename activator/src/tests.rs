#[cfg(test)]
pub mod tests {
    use doublezero_sdk::{AccountData, AccountType, DoubleZeroClient, MockDoubleZeroClient};
    use doublezero_sla_program::{
        pda::get_device_pda, pda::get_globalstate_pda, pda::get_tunnel_pda, pda::get_user_pda,
        state::globalstate::GlobalState,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    pub fn create_test_client() -> MockDoubleZeroClient {
        let mut client = MockDoubleZeroClient::new();

        // Program ID
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        // Global State
        let (globalstate_pubkey, _) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 0,
            account_index: 0,
            foundation_allowlist: vec![],
            device_allowlist: vec![],
            user_allowlist: vec![],
        };

        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));

        let payer = Pubkey::new_unique();
        client.expect_get_payer().returning(move || payer);

        client
    }

    pub fn get_device_bump_seed(client: &MockDoubleZeroClient) -> u8 {
        let (_, bump_seed) = get_device_pda(&client.get_program_id(), 0);
        bump_seed
    }

    pub fn get_tunnel_bump_seed(client: &MockDoubleZeroClient) -> u8 {
        let (_, bump_seed) = get_tunnel_pda(&client.get_program_id(), 0);
        bump_seed
    }

    pub fn get_user_bump_seed(client: &MockDoubleZeroClient) -> u8 {
        let (_, bump_seed) = get_user_pda(&client.get_program_id(), 0);
        bump_seed
    }
}
