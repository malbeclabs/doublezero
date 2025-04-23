use crate::requirements::{check_requirements, CHECK_BALANCE, CHECK_ID_JSON};
use clap::Args;
use doublezero_sdk::*;
use doublezero_sdk::commands::tunnel::get::GetTunnelCommand;
use doublezero_sdk::commands::tunnel::delete::DeleteTunnelCommand;

#[derive(Args, Debug)]
pub struct DeleteTunnelArgs {
    #[arg(long)]
    pub pubkey: String,
}

impl DeleteTunnelArgs {
    pub async fn execute(self, client: &DZClient) -> eyre::Result<()> {
        // Check requirements
        check_requirements(client, None, CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, tunnel) = GetTunnelCommand{ pubkey_or_code: self.pubkey }.execute(client)?;
        let _ = DeleteTunnelCommand{ index: tunnel.index }.execute(client)?;
            println!("Tunnel deleted");

        Ok(())
    }
}
