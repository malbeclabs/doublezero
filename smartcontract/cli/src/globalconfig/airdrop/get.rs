use crate::doublezerocommand::CliCommand;
use ::serde::Serialize;
use clap::Args;
use doublezero_sdk::GetGlobalStateCommand;
use std::io::Write;
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct GetAirdropCliCommand;

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
        let config_displays = vec![config_display];
        let table = Table::new(config_displays)
            .with(Style::psql().remove_horizontals())
            .to_string();
        writeln!(out, "{table}")?;

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

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = GetAirdropCliCommand.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,
            " contributor_airdrop_lamports | user_airdrop_lamports \n 1000000000                   | 40000                 \n"
        );
    }
}
