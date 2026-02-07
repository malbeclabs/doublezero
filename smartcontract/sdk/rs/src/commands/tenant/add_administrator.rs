use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    processors::tenant::add_administrator::TenantAddAdministratorArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

pub struct AddAdministratorTenantCommand {
    pub tenant_pubkey: Pubkey,
    pub administrator: Pubkey,
}

impl AddAdministratorTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        client.execute_transaction(
            DoubleZeroInstruction::TenantAddAdministrator(TenantAddAdministratorArgs {
                administrator: self.administrator,
            }),
            vec![
                AccountMeta::new(self.tenant_pubkey, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
        )
    }
}
