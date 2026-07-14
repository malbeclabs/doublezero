use crate::DoubleZeroClient;
use doublezero_serviceability::processors::globalstate::setairdrop::SetAirdropArgs;
use doublezero_serviceability_instruction::globalstate::set_airdrop;
use solana_sdk::signature::Signature;

#[derive(Clone, Debug, PartialEq)]
pub struct SetAirdropCommand {
    pub contributor_airdrop_lamports: Option<u64>,
    pub user_airdrop_lamports: Option<u64>,
}

impl SetAirdropCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(set_airdrop(
            &client.get_program_id(),
            &client.get_payer(),
            SetAirdropArgs {
                contributor_airdrop_lamports: self.contributor_airdrop_lamports,
                user_airdrop_lamports: self.user_airdrop_lamports,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::globalstate::setairdrop::SetAirdropCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::globalstate::setairdrop::SetAirdropArgs;
    use doublezero_serviceability_instruction::globalstate::set_airdrop;
    use mockall::predicate;
    use solana_sdk::signature::Signature;

    #[test]
    fn test_commands_setairdrop_command() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();

        let contributor_airdrop_lamports = Some(1_000_000_000);
        let user_airdrop_lamports = Some(40_000);

        let expected = set_airdrop(
            &program_id,
            &payer,
            SetAirdropArgs {
                contributor_airdrop_lamports,
                user_airdrop_lamports,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = SetAirdropCommand {
            contributor_airdrop_lamports,
            user_airdrop_lamports,
        }
        .execute(&client);
        assert!(res.is_ok());
    }
}
