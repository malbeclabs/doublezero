use crate::{
    doublezerocommand::CliCommand,
    helpers::resolve_multicastgroup_pk,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::{
    feed::list::ListFeedCommand,
    multicastgroup::{list::ListMulticastGroupCommand, update::UpdateMulticastGroupCommand},
};
use solana_sdk::pubkey::Pubkey;
use std::io::{BufRead, Write};
use tabled::{settings::Style, Table, Tabled};

#[derive(Args, Debug)]
pub struct FeedCountMigrateCliCommand {
    /// Multicast group pubkey or code to refresh (repeatable). Without it, every group is refreshed
    #[arg(long = "group", value_parser = validate_pubkey_or_code)]
    pub groups: Vec<String>,
    /// Refresh without asking for confirmation
    #[arg(short, long, default_value_t = false)]
    pub yes: bool,
    /// Print the wrong counts without changing them
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,
}

#[derive(Tabled)]
struct FeedCountChange {
    code: String,
    #[tabled(skip)]
    pubkey: Pubkey,
    #[tabled(rename = "feed count now")]
    stored: u32,
    #[tabled(rename = "feeds that list it")]
    counted: u32,
}

impl FeedCountMigrateCliCommand {
    pub async fn execute<C: CliCommand, W: Write, R: BufRead>(
        self,
        _ctx: &CliContext,
        client: &C,
        out: &mut W,
        input: &mut R,
    ) -> eyre::Result<()> {
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let groups = client.list_multicastgroup(ListMulticastGroupCommand)?;
        let feeds = client.list_feed(ListFeedCommand)?;
        let targets = if self.groups.is_empty() {
            groups.keys().copied().collect::<Vec<_>>()
        } else {
            self.groups
                .iter()
                .map(|group| resolve_multicastgroup_pk(client, group))
                .collect::<eyre::Result<Vec<_>>>()?
        };

        let mut changes = Vec::new();
        for pubkey in targets {
            let group = groups
                .get(&pubkey)
                .ok_or_else(|| eyre::eyre!("Multicast group not found: {pubkey}"))?;
            let counted = u32::try_from(
                feeds
                    .values()
                    .filter(|feed| feed.groups.contains(&pubkey))
                    .count(),
            )?;
            if counted != group.feed_count {
                changes.push(FeedCountChange {
                    code: group.code.clone(),
                    pubkey,
                    stored: group.feed_count,
                    counted,
                });
            }
        }
        if changes.is_empty() {
            writeln!(out, "Every feed count is correct.")?;
            return Ok(());
        }
        changes.sort_by(|left, right| left.code.cmp(&right.code));

        writeln!(
            out,
            "{}",
            Table::new(&changes).with(Style::psql().remove_horizontals())
        )?;
        if self.dry_run {
            return Ok(());
        }
        if !self.yes {
            write!(out, "Refresh {} feed count(s)? [y/N]: ", changes.len())?;
            out.flush()?;
            let mut answer = String::new();
            input.read_line(&mut answer)?;
            if !answer.trim().eq_ignore_ascii_case("y") {
                writeln!(out)?;
                eyre::bail!("the answer was not y, so nothing was changed");
            }
        }

        for change in &changes {
            let signature = client.update_multicastgroup(UpdateMulticastGroupCommand {
                pubkey: change.pubkey,
                code: None,
                multicast_ip: None,
                max_bandwidth: None,
                publisher_count: None,
                subscriber_count: None,
                owner: None,
                private: None,
                max_publishers: None,
                max_subscribers: None,
                feed_count: Some(change.counted),
            })?;
            writeln!(out, "{}: Signature: {signature}", change.code)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use doublezero_cli_core::testing::{block_on, cli_context_default_for_tests};

    use crate::{
        migrate::feed_count::FeedCountMigrateCliCommand, tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::multicastgroup::{
            get::GetMulticastGroupCommand, update::UpdateMulticastGroupCommand,
        },
        AccountType, Feed, MulticastGroup, MulticastGroupStatus,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    fn group(code: &str, feed_count: u32) -> MulticastGroup {
        MulticastGroup {
            account_type: AccountType::MulticastGroup,
            code: code.to_string(),
            multicast_ip: [233, 84, 178, 1].into(),
            max_bandwidth: 1_000_000_000,
            status: MulticastGroupStatus::Activated,
            feed_count,
            ..Default::default()
        }
    }

    fn feed(groups: Vec<Pubkey>) -> Feed {
        Feed {
            groups,
            ..Default::default()
        }
    }

    fn refresh(feed_count: u32, pubkey: Pubkey) -> UpdateMulticastGroupCommand {
        UpdateMulticastGroupCommand {
            pubkey,
            code: None,
            multicast_ip: None,
            max_bandwidth: None,
            publisher_count: None,
            subscriber_count: None,
            owner: None,
            private: None,
            max_publishers: None,
            max_subscribers: None,
            feed_count: Some(feed_count),
        }
    }

    // ewr-a is listed by two feeds but stores 0. ewr-b is listed by one feed and stores 1.
    fn client_with_two_groups(
        ewr_a: Pubkey,
        ewr_b: Pubkey,
    ) -> crate::doublezerocommand::MockCliCommand {
        let mut client = create_test_client();
        client.expect_check_requirements().returning(|_| Ok(()));
        client.expect_list_multicastgroup().returning(move |_| {
            Ok(HashMap::from([
                (ewr_a, group("ewr-a", 0)),
                (ewr_b, group("ewr-b", 1)),
            ]))
        });
        client.expect_list_feed().returning(move |_| {
            Ok(HashMap::from([
                (Pubkey::new_unique(), feed(vec![ewr_a, ewr_b])),
                (Pubkey::new_unique(), feed(vec![ewr_a])),
            ]))
        });
        client
    }

    #[test]
    fn test_cli_migrate_feed_count_fixes_every_wrong_count() {
        let (ewr_a, ewr_b) = (Pubkey::new_unique(), Pubkey::new_unique());
        let mut client = client_with_two_groups(ewr_a, ewr_b);
        let signature = Signature::new_unique();
        client
            .expect_update_multicastgroup()
            .with(predicate::eq(refresh(2, ewr_a)))
            .times(1)
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        block_on(
            FeedCountMigrateCliCommand {
                groups: vec![],
                yes: false,
                dry_run: false,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut output,
                &mut "y\n".as_bytes(),
            ),
        )
        .unwrap();
        assert_eq!(
            String::from_utf8(output).unwrap(),
            format!(
                " code  | feed count now | feeds that list it \n ewr-a | 0              | 2                  \nRefresh 1 feed count(s)? [y/N]: ewr-a: Signature: {signature}\n"
            )
        );
    }

    #[test]
    fn test_cli_migrate_feed_count_changes_nothing_on_a_correct_group() {
        let (ewr_a, ewr_b) = (Pubkey::new_unique(), Pubkey::new_unique());
        let mut client = client_with_two_groups(ewr_a, ewr_b);
        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: "ewr-b".to_string(),
            }))
            .returning(move |_| Ok((ewr_b, group("ewr-b", 1))));
        client.expect_update_multicastgroup().never();

        let mut output = Vec::new();
        block_on(
            FeedCountMigrateCliCommand {
                groups: vec!["ewr-b".to_string()],
                yes: false,
                dry_run: false,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut output,
                &mut "".as_bytes(),
            ),
        )
        .unwrap();
        assert_eq!(
            String::from_utf8(output).unwrap(),
            "Every feed count is correct.\n"
        );
    }

    #[test]
    fn test_cli_migrate_feed_count_changes_nothing_without_a_yes() {
        let (ewr_a, ewr_b) = (Pubkey::new_unique(), Pubkey::new_unique());
        let mut client = client_with_two_groups(ewr_a, ewr_b);
        client.expect_update_multicastgroup().never();

        let mut output = Vec::new();
        let err = block_on(
            FeedCountMigrateCliCommand {
                groups: vec![],
                yes: false,
                dry_run: false,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut output,
                &mut "n\n".as_bytes(),
            ),
        )
        .unwrap_err();
        assert_eq!(
            err.to_string(),
            "the answer was not y, so nothing was changed"
        );
    }

    #[test]
    fn test_cli_migrate_feed_count_dry_run_prints_the_wrong_counts_only() {
        let (ewr_a, ewr_b) = (Pubkey::new_unique(), Pubkey::new_unique());
        let mut client = client_with_two_groups(ewr_a, ewr_b);
        client.expect_update_multicastgroup().never();

        let mut output = Vec::new();
        block_on(
            FeedCountMigrateCliCommand {
                groups: vec![],
                yes: false,
                dry_run: true,
            }
            .execute(
                &cli_context_default_for_tests(),
                &client,
                &mut output,
                &mut "".as_bytes(),
            ),
        )
        .unwrap();
        assert_eq!(
            String::from_utf8(output).unwrap(),
            " code  | feed count now | feeds that list it \n ewr-a | 0              | 2                  \n"
        );
    }
}
