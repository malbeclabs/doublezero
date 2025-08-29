pub mod utils {
    use solana_sdk::pubkey::Pubkey;

    use crate::doublezerocommand::MockCliCommand;

    pub fn create_test_client() -> MockCliCommand {
        let mut client = MockCliCommand::new();
        // Payer
        let payer: Pubkey = Pubkey::from_str_const("DDddB7bhR9azxLAUEH7ZVtW168wRdreiDKhi4McDfKZt");
        let program_id = Pubkey::from_str_const("GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah");
        client.expect_get_payer().returning(move || payer);
        client.expect_get_balance().returning(|| Ok(10));
        client.expect_get_epoch().returning(|| Ok(10));
        client.expect_get_program_id().returning(move || program_id);

        client
    }
}
