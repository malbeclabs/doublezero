use crate::helpers::parse_pubkey;
use clap::Args;
use doublezero_sdk::*;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetAccountArgs {
    #[arg(long)]
    pub pubkey: String,
    #[arg(long, action = clap::ArgAction::SetTrue)]
    pub logs: bool,
}

impl GetAccountArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        let pubkey = parse_pubkey(&self.pubkey).expect("Invalid pubkey");

        match client.get(pubkey) {
            Ok(account) => {
                writeln!(out, "{} ({})", account.get_name(), account.get_args())?;
                writeln!(out, "")?;

                match client.get_transactions(pubkey) {
                    Ok(trans) => {
                        writeln!(out, "Transactions:")?;
                        for tran in trans {
                            writeln!(
                                out,
                                "{}: {} ({})\n\t\t\tpubkey: {}, signature: {}",
                                &tran.time.to_string(),
                                tran.instruction.get_name(),
                                tran.instruction.get_args(),
                                tran.account,
                                tran.signature
                            )?;

                            if self.logs {
                                for msg in tran.log_messages {
                                    writeln!(out, "  - {}", msg)?;
                                }
                                writeln!(out, "")?;
                            }
                        }
                    }
                    Err(e) => {
                        writeln!(out, "Error: {}", e)?;
                    }
                }
            }
            Err(e) => {
                writeln!(out, "Error: {}", e)?;
            }
        }

        Ok(())
    }
}
