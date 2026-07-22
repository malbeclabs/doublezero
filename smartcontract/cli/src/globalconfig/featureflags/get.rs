use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_cli_core::{render_record, CliContext, OutputFormat};
use doublezero_sdk::GetGlobalStateCommand;
use doublezero_serviceability::state::feature_flags::enabled_flags;
use serde::Serialize;
use std::io::Write;
use tabled::Tabled;

#[derive(Args, Debug)]
pub struct GetFeatureFlagsCliCommand {
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct FeatureFlagsDisplay {
    flags: String,
    raw: u128,
}

impl GetFeatureFlagsCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        let (_, gstate) = client.get_globalstate(GetGlobalStateCommand)?;

        let flags = enabled_flags(gstate.feature_flags);
        let flag_names: Vec<String> = flags.iter().map(|f| f.to_string()).collect();

        let display = FeatureFlagsDisplay {
            flags: if flag_names.is_empty() {
                String::new()
            } else {
                flag_names.join(", ")
            },
            raw: gstate.feature_flags,
        };

        render_record(out, &display, OutputFormat::from_flags(self.json, false))
    }
}

#[cfg(test)]
mod tests {
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};

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
            feed_authority_pk: Pubkey::default(),
            min_compatible_version: Default::default(),
        };

        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, globalstate.clone())));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let cmd = GetFeatureFlagsCliCommand { json: false };
        let res = block_on(cmd.execute(&ctx, &client, &mut output));
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.contains("onchain-allocation"));
        assert!(output_str.contains("1"));
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
            feed_authority_pk: Pubkey::default(),
            min_compatible_version: Default::default(),
        };

        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, globalstate.clone())));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let cmd = GetFeatureFlagsCliCommand { json: false };
        let res = block_on(cmd.execute(&ctx, &client, &mut output));
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        // Table output: empty flags column, raw = 0
        assert!(output_str.contains("0"));
    }

    #[test]
    fn test_cli_globalconfig_featureflags_get_json() {
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
            feed_authority_pk: Pubkey::default(),
            min_compatible_version: Default::default(),
        };

        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, globalstate.clone())));

        let mut output = Vec::new();
        let ctx = cli_context_default_for_tests();
        let cmd = GetFeatureFlagsCliCommand { json: true };
        let res = block_on(cmd.execute(&ctx, &client, &mut output));
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        let parsed: serde_json::Value = serde_json::from_str(&output_str).unwrap();
        assert_eq!(parsed["raw"], 1);
        assert!(parsed["flags"]
            .as_str()
            .unwrap()
            .contains("onchain-allocation"));
    }
}
