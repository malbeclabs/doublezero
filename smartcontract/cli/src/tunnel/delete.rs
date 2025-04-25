use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::commands::tunnel::delete::DeleteTunnelCommand;
use doublezero_sdk::commands::tunnel::get::GetTunnelCommand;
use doublezero_sdk::*;

#[derive(Args, Debug)]
pub struct DeleteTunnelArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteTunnelArgs {
    pub fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, tunnel) = GetTunnelCommand {
            pubkey_or_code: self.pubkey,
        }
        .execute(client)?;
        let signature = DeleteTunnelCommand {
            index: tunnel.index,
        }
        .execute(client)?;
        println!("Signature: {}", signature);

        Ok(())
    }
}
