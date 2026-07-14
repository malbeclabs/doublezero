use crate::DoubleZeroClient;
use doublezero_serviceability::processors::tenant::remove_administrator::TenantRemoveAdministratorArgs;
use doublezero_serviceability_instruction::tenant::tenant_remove_administrator;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

pub struct RemoveAdministratorTenantCommand {
    pub tenant_pubkey: Pubkey,
    pub administrator: Pubkey,
}

impl RemoveAdministratorTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(tenant_remove_administrator(
            &client.get_program_id(),
            &client.get_payer(),
            &self.tenant_pubkey,
            TenantRemoveAdministratorArgs {
                administrator: self.administrator,
            },
        ))
    }
}
