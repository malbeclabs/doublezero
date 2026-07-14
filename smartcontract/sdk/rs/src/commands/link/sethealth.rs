use crate::DoubleZeroClient;
use doublezero_serviceability::{
    processors::link::sethealth::LinkSetHealthArgs, state::link::LinkHealth,
};
use doublezero_serviceability_instruction::link::set_link_health;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct SetLinkHealthCommand {
    pub pubkey: Pubkey,
    pub health: LinkHealth,
}

impl SetLinkHealthCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        client.send_transaction(set_link_health(
            &client.get_program_id(),
            &client.get_payer(),
            &self.pubkey,
            LinkSetHealthArgs {
                health: self.health,
            },
        ))
    }
}
