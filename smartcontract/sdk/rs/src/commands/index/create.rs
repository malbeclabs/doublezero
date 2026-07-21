use crate::DoubleZeroClient;
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{pda::get_index_pda, processors::index::create::IndexCreateArgs};
use doublezero_serviceability_instruction::index::create_index;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateIndexCommand {
    pub entity_seed: String,
    pub key: String,
    pub entity_pubkey: Pubkey,
}

impl CreateIndexCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let key =
            validate_account_code(&self.key).map_err(|err| eyre::eyre!("invalid key: {err}"))?;

        let program_id = client.get_program_id();
        let (index_pda, _) = get_index_pda(&program_id, self.entity_seed.as_bytes(), &key);

        // The builder derives the index and globalstate PDAs.
        client
            .send_transaction(create_index(
                &program_id,
                &client.get_payer(),
                &self.entity_pubkey,
                IndexCreateArgs {
                    entity_seed: self.entity_seed.clone(),
                    key,
                },
            ))
            .map(|sig| (sig, index_pda))
    }
}
