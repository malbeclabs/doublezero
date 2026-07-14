use crate::DoubleZeroClient;
use doublezero_serviceability::processors::tenant::add_administrator::TenantAddAdministratorArgs;
use doublezero_serviceability_instruction::tenant::tenant_add_administrator;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

pub struct AddAdministratorTenantCommand {
    pub tenant_pubkey: Pubkey,
    pub administrator: Pubkey,
}

impl AddAdministratorTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(tenant_add_administrator(
            &client.get_program_id(),
            &client.get_payer(),
            &self.tenant_pubkey,
            TenantAddAdministratorArgs {
                administrator: self.administrator,
            },
        ))
    }
}
