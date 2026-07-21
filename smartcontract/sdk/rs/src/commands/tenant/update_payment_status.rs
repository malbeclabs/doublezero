use crate::DoubleZeroClient;
use doublezero_serviceability::processors::tenant::update_payment_status::UpdatePaymentStatusArgs;
use doublezero_serviceability_instruction::tenant::update_payment_status;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdatePaymentStatusCommand {
    pub tenant_pubkey: Pubkey,
    pub payment_status: u8,
    pub last_deduction_dz_epoch: Option<u64>,
}

impl UpdatePaymentStatusCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(update_payment_status(
            &client.get_program_id(),
            &client.get_payer(),
            &self.tenant_pubkey,
            UpdatePaymentStatusArgs {
                payment_status: self.payment_status,
                last_deduction_dz_epoch: self.last_deduction_dz_epoch,
            },
        ))
    }
}
