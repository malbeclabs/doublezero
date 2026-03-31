use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_index_pda,
    processors::index::create::IndexCreateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateIndexCommand {
    pub entity_seed: String,
    pub code: String,
    pub entity_pubkey: Pubkey,
}

impl CreateIndexCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        let (globalstate_pubkey, _) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (index_pda, _) =
            get_index_pda(&client.get_program_id(), self.entity_seed.as_bytes(), &code);

        let accounts = vec![
            AccountMeta::new(index_pda, false),
            AccountMeta::new_readonly(self.entity_pubkey, false),
            AccountMeta::new_readonly(globalstate_pubkey, false),
        ];

        client
            .execute_transaction(
                DoubleZeroInstruction::CreateIndex(IndexCreateArgs {
                    entity_seed: self.entity_seed.clone(),
                    code,
                }),
                accounts,
            )
            .map(|sig| (sig, index_pda))
    }
}
