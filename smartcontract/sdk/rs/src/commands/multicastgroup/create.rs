use doublezero_sla_program::{
    instructions::DoubleZeroInstruction, pda::get_multicastgroup_pda,
    processors::multicastgroup::create::MulticastGroupCreateArgs, types::IpV4,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateMulticastGroupCommand {
    pub code: String,
    pub multicast_ip: IpV4,
    pub max_bandwidth: u64,
    pub owner: Pubkey,
}

impl CreateMulticastGroupCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand {}
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, bump_seed) =
            get_multicastgroup_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateMulticastGroup(MulticastGroupCreateArgs {
                    index: globalstate.account_index + 1,
                    bump_seed,
                    code: self.code.to_string(),
                    multicast_ip: self.multicast_ip,
                    max_bandwidth: self.max_bandwidth,
                    owner: self.owner,
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::multicastgroup::create::CreateMulticastGroupCommand,
        tests::tests::create_test_client, DoubleZeroClient,
    };
    use doublezero_sla_program::{
        instructions::DoubleZeroInstruction,
        pda::{get_globalstate_pda, get_multicastgroup_pda},
        processors::multicastgroup::create::MulticastGroupCreateArgs,
    };
    use mockall::predicate;
    use solana_sdk::{instruction::AccountMeta, signature::Signature};

    #[test]
    fn test_commands_multicastgroup_create_command() {
        let mut client = create_test_client();

        let (globalstate_pubkey, _globalstate) = get_globalstate_pda(&client.get_program_id());
        let (pda_pubkey, bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);

        client
            .expect_execute_transaction()
            .with(
                predicate::eq(DoubleZeroInstruction::CreateMulticastGroup(
                    MulticastGroupCreateArgs {
                        index: 1,
                        bump_seed,
                        code: "test".to_string(),
                        multicast_ip: [10, 0, 0, 1],
                        max_bandwidth: 1000,
                        owner: globalstate_pubkey,
                    },
                )),
                predicate::eq(vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ]),
            )
            .returning(|_, _| Ok(Signature::new_unique()));

        let res = CreateMulticastGroupCommand {
            code: "test".to_string(),
            multicast_ip: [10, 0, 0, 1],
            max_bandwidth: 1000,
            owner: globalstate_pubkey,
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
