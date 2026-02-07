use crate::DoubleZeroClient;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::tenant::delete::TenantDeleteArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct DeleteTenantCommand {
    pub tenant_pubkey: Pubkey,
}

impl DeleteTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::DeleteTenant(TenantDeleteArgs {}),
            vec![
                AccountMeta::new(self.tenant_pubkey, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
        )
    }
}
