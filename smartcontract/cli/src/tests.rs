pub mod utils {
    use doublezero_cli_core::{CliContext, CliContextBuilder};
    use doublezero_config::Environment;
    use solana_sdk::pubkey::Pubkey;

    use crate::doublezerocommand::MockCliCommand;

    /// Minimal context for verb tests. Commands that ignore `ctx` only need a
    /// well-formed value; `Local` sources all URLs/program-IDs from config.
    pub fn create_test_context() -> CliContext {
        CliContextBuilder::new()
            .with_env(Environment::Local)
            .build()
            .expect("test context")
    }

    pub fn create_test_client() -> MockCliCommand {
        let mut client = MockCliCommand::new();
        // Payer
        let payer: Pubkey = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");
        let program_id = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        client.expect_get_payer().returning(move || payer);
        client.expect_get_balance().returning(|| Ok(10));
        client.expect_get_epoch().returning(|| Ok(10));
        client.expect_get_program_id().returning(move || program_id);
        // Pre-flight checks call has_keypair_source() to decide whether to skip
        // the default-path keypair file check. Tests provide the keypair via the
        // mock, so report a source as present.
        client.expect_has_keypair_source().returning(|| true);

        client
    }
}
