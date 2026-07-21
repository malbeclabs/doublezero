use crate::DoubleZeroClient;
use doublezero_serviceability::{
    processors::tenant::update::TenantUpdateArgs, state::tenant::TenantBillingConfig,
};
use doublezero_serviceability_instruction::tenant::update_tenant;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateTenantCommand {
    pub tenant_pubkey: Pubkey,
    pub vrf_id: Option<u16>,
    pub token_account: Option<Pubkey>,
    pub metro_routing: Option<bool>,
    pub route_liveness: Option<bool>,
    pub billing: Option<TenantBillingConfig>,
    pub include_topologies: Option<Vec<Pubkey>>,
}

impl UpdateTenantCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(update_tenant(
            &client.get_program_id(),
            &client.get_payer(),
            &self.tenant_pubkey,
            TenantUpdateArgs {
                vrf_id: self.vrf_id,
                token_account: self.token_account,
                metro_routing: self.metro_routing,
                route_liveness: self.route_liveness,
                billing: self.billing,
                include_topologies: self.include_topologies.clone(),
            },
        ))
    }
}
