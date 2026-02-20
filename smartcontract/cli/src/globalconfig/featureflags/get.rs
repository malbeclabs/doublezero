use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::GetGlobalStateCommand;
use doublezero_serviceability::state::globalstate::enabled_flags;
use std::io::Write;

#[derive(Args, Debug)]
pub struct GetFeatureFlagsCliCommand;

impl GetFeatureFlagsCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, gstate) = client.get_globalstate(GetGlobalStateCommand)?;

        let flags = enabled_flags(gstate.feature_flags);
        let flag_names: Vec<String> = flags.iter().map(|f| f.to_string()).collect();

        if flag_names.is_empty() {
            writeln!(
                out,
                "No feature flags enabled (raw: {})",
                gstate.feature_flags
            )?;
        } else {
            writeln!(
                out,
                "Enabled feature flags: {} (raw: {})",
                flag_names.join(", "),
                gstate.feature_flags
            )?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        globalconfig::featureflags::get::GetFeatureFlagsCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{AccountType, GetGlobalStateCommand, GlobalState};
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_globalconfig_featureflags_get_enabled() {
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
            contributor_airdrop_lamports: 0,
            user_airdrop_lamports: 0,
            health_oracle_pk: Pubkey::default(),
            qa_allowlist: vec![],
            feature_flags: 1,
        };

        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, globalstate.clone())));

        let mut output = Vec::new();
        let res = GetFeatureFlagsCliCommand.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("onchain-allocation"));
        assert!(output_str.contains("raw: 1"));
    }

    #[test]
    fn test_cli_globalconfig_featureflags_get_none() {
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
            contributor_airdrop_lamports: 0,
            user_airdrop_lamports: 0,
            health_oracle_pk: Pubkey::default(),
            qa_allowlist: vec![],
            feature_flags: 0,
        };

        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, globalstate.clone())));

        let mut output = Vec::new();
        let res = GetFeatureFlagsCliCommand.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("No feature flags enabled"));
    }
}
