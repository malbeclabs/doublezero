use crate::doublezerocommand::CliCommand;
use ::serde::Serialize;
use clap::Args;
use doublezero_sdk::GetGlobalStateCommand;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetAirdropCliCommand {
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
pub struct AirdropDisplay {
    pub contributor_airdrop_lamports: u64,
    pub user_airdrop_lamports: u64,
}

impl GetAirdropCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, gstate) = client.get_globalstate(GetGlobalStateCommand)?;

        let config_display = AirdropDisplay {
            contributor_airdrop_lamports: gstate.contributor_airdrop_lamports,
            user_airdrop_lamports: gstate.user_airdrop_lamports,
        };

        if self.json {
            let json = serde_json::to_string_pretty(&config_display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = AirdropDisplay::headers();
            let fields = config_display.fields();
            let max_len = headers.iter().map(|h| h.len()).max().unwrap_or(0);
            for (header, value) in headers.iter().zip(fields.iter()) {
                writeln!(out, " {header:<max_len$} | {value}")?;
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        globalconfig::airdrop::get::GetAirdropCliCommand, tests::utils::create_test_client,
    };
    use doublezero_sdk::{AccountType, GetGlobalStateCommand, GlobalState};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_globalconfig_airdrop_get() {
        let mut client = create_test_client();

        let gstate_pubkey = Pubkey::new_unique();
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 0,
            account_index: 0,
            foundation_allowlist: vec![],
            _device_allowlist: vec![],
            _user_allowlist: vec![],
            activator_authority_pk: Pubkey::default(),
            sentinel_authority_pk: Pubkey::default(),
            contributor_airdrop_lamports: 1_000_000_000,
            user_airdrop_lamports: 40_000,
            health_oracle_pk: Pubkey::default(),
            qa_allowlist: vec![],
            feature_flags: 0,
            reservation_authority_pk: Pubkey::default(),
        };

        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, globalstate.clone())));

        // Table output
        let mut output = Vec::new();
        let res = GetAirdropCliCommand { json: false }.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("contributor_airdrop_lamports", "1000000000"),
            "contributor_airdrop_lamports row should contain value"
        );
        assert!(
            has_row("user_airdrop_lamports", "40000"),
            "user_airdrop_lamports row should contain value"
        );

        // JSON output
        let mut output = Vec::new();
        let res = GetAirdropCliCommand { json: true }.execute(&client, &mut output);
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(
            json["contributor_airdrop_lamports"].as_u64().unwrap(),
            1_000_000_000
        );
        assert_eq!(json["user_airdrop_lamports"].as_u64().unwrap(), 40_000);
    }
}
