use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, processors::globalstate::setairdrop::SetAirdropArgs,
};
use solana_sdk::{instruction::AccountMeta, signature::Signature};

#[derive(Clone, Debug, PartialEq)]
pub struct SetAirdropCommand {
    pub contributor_airdrop_lamports: Option<u64>,
    pub user_airdrop_lamports: Option<u64>,
}

impl SetAirdropCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("GlobalState not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::SetAirdrop(SetAirdropArgs {
                contributor_airdrop_lamports: self.contributor_airdrop_lamports,
                user_airdrop_lamports: self.user_airdrop_lamports,
            }),
            vec![AccountMeta::new(globalstate_pubkey, false)],
        )
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::globalstate::setairdrop::SetAirdropCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::{
        instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
        processors::globalstate::setairdrop::SetAirdropArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_setairdrop_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());

        let contributor_airdrop_lamports = Some(1_000_000_000);
        let user_airdrop_lamports = Some(40_000);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::SetAirdrop(SetAirdropArgs {
                    contributor_airdrop_lamports,
                    user_airdrop_lamports,
                })),
                predicate::eq(vec![AccountMeta::new(globalstate_pubkey, false)]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = SetAirdropCommand {
            contributor_airdrop_lamports,
            user_airdrop_lamports,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
