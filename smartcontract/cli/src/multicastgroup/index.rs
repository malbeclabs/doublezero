use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::{
    index::create::CreateIndexCommand, multicastgroup::get::GetMulticastGroupCommand,
};
use doublezero_serviceability::seeds::SEED_MULTICAST_GROUP;
use std::io::Write;

/// Claims a group's code in the Index, so no group created on a bond can take the same code.
///
/// A group created on a bond claims its code itself. A group an admin creates does not, so an
/// operator runs this once for each of those. The code comes from the group rather than from a
/// flag, so the entry cannot name a code the group does not have.
#[derive(Args, Debug)]
pub struct IndexMulticastGroupCliCommand {
    /// Multicast group Pubkey or code
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
}

impl IndexMulticastGroupCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let (pubkey, mgroup) = client.get_multicastgroup(GetMulticastGroupCommand {
            pubkey_or_code: self.pubkey,
        })?;
        let (signature, index) = client.create_index(CreateIndexCommand {
            entity_seed: String::from_utf8(SEED_MULTICAST_GROUP.to_vec())?,
            key: mgroup.code.clone(),
            entity_pubkey: pubkey,
        })?;
        writeln!(
            out,
            "Index {index} claims code {} for {pubkey}",
            mgroup.code
        )?;
        writeln!(out, "Signature: {signature}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        multicastgroup::index::IndexMulticastGroupCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};
    use doublezero_sdk::{
        commands::{
            index::create::CreateIndexCommand, multicastgroup::get::GetMulticastGroupCommand,
        },
        MulticastGroup,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_multicastgroup_index_claims_the_groups_own_code() {
        let mut client = create_test_client();
        let group = Pubkey::new_unique();
        let index = Pubkey::new_unique();

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: "Tokyo-TOB".to_string(),
            }))
            .returning(move |_| {
                Ok((
                    group,
                    MulticastGroup {
                        code: "Tokyo-TOB".to_string(),
                        ..MulticastGroup::default()
                    },
                ))
            });
        client
            .expect_create_index()
            .with(predicate::eq(CreateIndexCommand {
                entity_seed: "multicast".to_string(),
                key: "Tokyo-TOB".to_string(),
                entity_pubkey: group,
            }))
            .times(1)
            .returning(move |_| Ok((Signature::new_unique(), index)));

        let mut out = Vec::new();
        let res = block_on(
            IndexMulticastGroupCliCommand {
                pubkey: "Tokyo-TOB".to_string(),
            }
            .execute(&cli_context_default_for_tests(), &client, &mut out),
        );
        assert!(res.is_ok(), "{res:?}");
        let out = String::from_utf8(out).unwrap();
        assert!(out.contains(&index.to_string()), "{out}");
    }
}
