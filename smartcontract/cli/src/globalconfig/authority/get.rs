use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::GetGlobalStateCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetAuthorityCliCommand {
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
pub struct AuthorityDisplay {
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub activator_authority: Pubkey,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub sentinel_authority: Pubkey,
    #[serde(serialize_with = "serializer::serialize_pubkey_as_string")]
    pub health_oracle: Pubkey,
}

impl GetAuthorityCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, gstate) = client.get_globalstate(GetGlobalStateCommand)?;

        let config_display = AuthorityDisplay {
            activator_authority: gstate.activator_authority_pk,
            sentinel_authority: gstate.sentinel_authority_pk,
            health_oracle: gstate.health_oracle_pk,
        };

        if self.json {
            let json = serde_json::to_string_pretty(&config_display)?;
            writeln!(out, "{json}")?;
        } else {
            let headers = AuthorityDisplay::headers();
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
        globalconfig::authority::get::GetAuthorityCliCommand, tests::utils::create_test_client,
    };
    use doublezero_sdk::{AccountType, GetGlobalStateCommand, GlobalState};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_globalconfig_authority_get() {
        let mut client = create_test_client();

        let gstate_pubkey = Pubkey::new_unique();
        let activator_authority = Pubkey::new_unique();
        let sentinel_authority = Pubkey::new_unique();
        let globalstate = GlobalState {
            account_type: AccountType::GlobalState,
            bump_seed: 0,
            account_index: 0,
            foundation_allowlist: vec![],
            _device_allowlist: vec![],
            _user_allowlist: vec![],
            activator_authority_pk: activator_authority,
            sentinel_authority_pk: sentinel_authority,
            contributor_airdrop_lamports: 0,
            user_airdrop_lamports: 0,
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
        let res = GetAuthorityCliCommand { json: false }.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let has_row = |header: &str, value: &str| {
            output_str
                .lines()
                .any(|l| l.contains(header) && l.contains(value))
        };
        assert!(
            has_row("activator_authority", &activator_authority.to_string()),
            "activator_authority row should contain value"
        );
        assert!(
            has_row("access_authority", &sentinel_authority.to_string()),
            "access_authority row should contain value"
        );

        // JSON output
        let mut output = Vec::new();
        let res = GetAuthorityCliCommand { json: true }.execute(&client, &mut output);
        assert!(res.is_ok());
        let json: serde_json::Value =
            serde_json::from_str(&String::from_utf8(output).unwrap()).unwrap();
        assert_eq!(
            json["activator_authority"].as_str().unwrap(),
            activator_authority.to_string()
        );
        assert_eq!(
            json["access_authority"].as_str().unwrap(),
            sentinel_authority.to_string()
        );
    }
}
