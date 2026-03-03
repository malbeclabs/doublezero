use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_program_common::serializer;
use doublezero_sdk::GetGlobalStateCommand;
use serde::Serialize;
use solana_sdk::pubkey::Pubkey;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

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
    pub access_authority: Pubkey,
}

impl GetAuthorityCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, gstate) = client.get_globalstate(GetGlobalStateCommand)?;

        let config_display = AuthorityDisplay {
            activator_authority: gstate.activator_authority_pk,
            access_authority: gstate.sentinel_authority_pk,
        };

        if self.json {
            let json = serde_json::to_string_pretty(&config_display)?;
            writeln!(out, "{json}")?;
        } else {
            let table = Table::new([config_display])
                .with(Style::psql().remove_horizontals())
                .to_string();
            writeln!(out, "{table}")?;
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
        assert!(
            output_str.contains(&activator_authority.to_string()),
            "should contain activator authority"
        );
        assert!(
            output_str.contains(&sentinel_authority.to_string()),
            "should contain sentinel authority"
        );

        // JSON output
        let mut output = Vec::new();
        let res = GetAuthorityCliCommand { json: true }.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("\"activator_authority\""),
            "should contain activator_authority key"
        );
        assert!(
            output_str.contains("\"access_authority\""),
            "should contain access_authority key"
        );
    }
}
