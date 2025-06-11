pub mod utils {
    use doublezero_serviceability::{
        pda::get_globalstate_pda,
        state::{accountdata::AccountData, accounttype::AccountType, globalstate::GlobalState},
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::env;
    use tempfile::TempDir;

    use crate::{
        config::{write_doublezero_config, ClientConfig},
        create_new_pubkey_user, MockDoubleZeroClient,
    };

    pub fn create_test_client() -> MockDoubleZeroClient {
        let mut client = MockDoubleZeroClient::new();

        // Payer
        let payer = Pubkey::new_unique();
        client.expect_get_payer().returning(move || payer);
        // Program ID
        let program_id = Pubkey::new_unique();
        client.expect_get_program_id().returning(move || program_id);

        // Global State
        let (globalstate_pubkey, bump_seed) = get_globalstate_pda(&program_id);
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed,
            account_index: 0,
            foundation_allowlist: vec![],
            device_allowlist: vec![],
            user_allowlist: vec![],
        };
        client
            .expect_get()
            .with(predicate::eq(globalstate_pubkey))
            .returning(move |_| Ok(AccountData::GlobalState(globalstate.clone())));
        client
            .expect_execute_transaction()
            .returning(|_, _| Ok(Signature::new_unique()));
        client
    }

    pub fn create_temp_config() -> eyre::Result<TempDir> {
        let tmpdir = TempDir::with_prefix("doublezero-tests-").unwrap();
        env::set_var("DOUBLEZERO_CONFIG_FILE", tmpdir.path().join("config.yaml"));
        let client_cfg = ClientConfig {
            keypair_path: tmpdir.path().join("id.json").to_str().unwrap().to_string(),
            ..Default::default()
        };
        write_doublezero_config(&client_cfg)?;
        create_new_pubkey_user(false)?;
        Ok(tmpdir)
    }
}
