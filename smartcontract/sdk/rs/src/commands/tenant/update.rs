use crate::DoubleZeroClient;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction, pda::get_globalstate_pda,
    processors::tenant::update::TenantUpdateArgs,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateTenantCommand {
    pub tenant_pubkey: Pubkey,
    pub vrf_id: Option<u16>,
    pub token_account: Option<Pubkey>,
    pub metro_route: Option<bool>,
    pub route_aliveness: Option<bool>,
}

impl UpdateTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (globalstate_pubkey, _) = get_globalstate_pda(&client.get_program_id());

        client.execute_transaction(
            DoubleZeroInstruction::UpdateTenant(TenantUpdateArgs {
                vrf_id: self.vrf_id,
                token_account: self.token_account,
                metro_route: self.metro_route,
                route_aliveness: self.route_aliveness,
            }),
            vec![
                AccountMeta::new(self.tenant_pubkey, false),
                AccountMeta::new_readonly(globalstate_pubkey, false),
            ],
        )
    }
}
