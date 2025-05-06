use crate::helpers::parse_pubkey;
use clap::Args;
use doublezero_sdk::*;

#[derive(Args, Debug)]
pub struct GetAccountArgs {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long, action = clap::ArgAction::SetTrue)]
    pub logs: bool,
}

impl GetAccountArgs {
    pub fn execute(self, client: &dyn DoubleZeroClient) -> eyre::Result<()> {
        // Check requirements
        let pubkey = parse_pubkey(&self.pubkey).expect("Invalid pubkey");

        match client.get(pubkey) {
            Ok(account) => {
                println!("{} ({})", account.get_name(), account.get_args());
                println!();

                match client.get_transactions(pubkey) {
                    Ok(trans) => {
                        println!("Transactions:");
                        for tran in trans {
                            println!(
                                "{}: {} ({})\n\t\t\tpubkey: {}, signature: {}",
                                &tran.time.to_string(),
                                tran.instruction.get_name(),
                                tran.instruction.get_args(),
                                tran.account,
                                tran.signature
                            );

                            if self.logs {
                                for msg in tran.log_messages {
                                    println!("  - {}", msg);
                                }
                                println!();
                            }
                        }
                    }
                    Err(e) => {
                        println!("Error: {}", e);
                    }
                }
            }
            Err(e) => {
                println!("Error: {}", e);
            }
        }

        Ok(())
    }
}
