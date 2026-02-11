use crate::DoubleZeroClient;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::tenant::update_payment_status::UpdatePaymentStatusArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdatePaymentStatusCommand {
    pub tenant_pubkey: Pubkey,
    pub payment_status: u8,
}

impl UpdatePaymentStatusCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::UpdatePaymentStatus(UpdatePaymentStatusArgs {
                payment_status: self.payment_status,
            }),
            vec![
                AccountMeta::new(self.tenant_pubkey, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
        )
    }
}
