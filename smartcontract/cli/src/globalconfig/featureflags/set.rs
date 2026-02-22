use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::{
    commands::globalstate::setfeatureflags::SetFeatureFlagsCommand, GetGlobalStateCommand,
};
use doublezero_serviceability::state::feature_flags::FeatureFlag;
use std::io::Write;

#[derive(Args, Debug)]
pub struct SetFeatureFlagsCliCommand {
    /// Feature flags to enable (comma-separated, e.g. --enable onchain-allocation)
    #[arg(long, value_delimiter = ',')]
    pub enable: Vec<String>,

    /// Feature flags to disable (comma-separated, e.g. --disable onchain-allocation)
    #[arg(long, value_delimiter = ',')]
    pub disable: Vec<String>,
}

impl SetFeatureFlagsCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        if self.enable.is_empty() && self.disable.is_empty() {
            return Err(eyre::eyre!(
                "at least one of --enable or --disable must be provided"
            ));
        }

        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (_, gstate) = client.get_globalstate(GetGlobalStateCommand)?;
        let mut mask = gstate.feature_flags;

        for flag_str in &self.enable {
            let flag: FeatureFlag = flag_str.parse().map_err(|e: String| eyre::eyre!(e))?;
            mask |= flag.to_mask();
        }

        for flag_str in &self.disable {
            let flag: FeatureFlag = flag_str.parse().map_err(|e: String| eyre::eyre!(e))?;
            mask &= !flag.to_mask();
        }

        let signature = client.set_feature_flags(SetFeatureFlagsCommand {
            feature_flags: mask,
        })?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        globalconfig::featureflags::set::SetFeatureFlagsCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::globalstate::setfeatureflags::SetFeatureFlagsCommand, AccountType,
        GetGlobalStateCommand, GlobalState,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    fn test_signature() -> Signature {
        Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ])
    }

    fn test_globalstate(feature_flags: u128) -> GlobalState {
        GlobalState {
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
            feature_flags,
        }
    }

    #[test]
    fn test_cli_globalconfig_featureflags_set_enable() {
        let mut client = create_test_client();
        let signature = test_signature();
        let gstate_pubkey = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, test_globalstate(0))));
        client
            .expect_set_feature_flags()
            .with(predicate::eq(SetFeatureFlagsCommand { feature_flags: 1 }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetFeatureFlagsCliCommand {
            enable: vec!["onchain-allocation".to_string()],
            disable: vec![],
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.starts_with("Signature: "));
    }

    #[test]
    fn test_cli_globalconfig_featureflags_set_disable() {
        let mut client = create_test_client();
        let signature = test_signature();
        let gstate_pubkey = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, test_globalstate(1))));
        client
            .expect_set_feature_flags()
            .with(predicate::eq(SetFeatureFlagsCommand { feature_flags: 0 }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetFeatureFlagsCliCommand {
            enable: vec![],
            disable: vec!["onchain-allocation".to_string()],
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert!(output_str.starts_with("Signature: "));
    }

    #[test]
    fn test_cli_globalconfig_featureflags_set_enable_and_disable() {
        let mut client = create_test_client();
        let signature = test_signature();
        let gstate_pubkey = Pubkey::new_unique();

        // Start with onchain-allocation enabled (mask=1), disable it
        // In a real scenario with multiple flags, enable one and disable another
        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, test_globalstate(1))));
        client
            .expect_set_feature_flags()
            .with(predicate::eq(SetFeatureFlagsCommand { feature_flags: 0 }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = SetFeatureFlagsCliCommand {
            enable: vec![],
            disable: vec!["onchain-allocation".to_string()],
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
    }

    #[test]
    fn test_cli_globalconfig_featureflags_set_no_flags_error() {
        let client = create_test_client();

        let mut output = Vec::new();
        let res = SetFeatureFlagsCliCommand {
            enable: vec![],
            disable: vec![],
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
        assert!(res
            .unwrap_err()
            .to_string()
            .contains("at least one of --enable or --disable must be provided"));
    }

    #[test]
    fn test_cli_globalconfig_featureflags_set_unknown_flag() {
        let mut client = create_test_client();
        let gstate_pubkey = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_globalstate()
            .with(predicate::eq(GetGlobalStateCommand))
            .returning(move |_| Ok((gstate_pubkey, test_globalstate(0))));

        let mut output = Vec::new();
        let res = SetFeatureFlagsCliCommand {
            enable: vec!["unknown-flag".to_string()],
            disable: vec![],
        }
        .execute(&client, &mut output);
        assert!(res.is_err());
    }
}
