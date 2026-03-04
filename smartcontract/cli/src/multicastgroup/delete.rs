use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
    validators::validate_pubkey_or_code,
};
use clap::Args;
use doublezero_sdk::commands::{
    accesspass::list::ListAccessPassCommand,
    multicastgroup::{
        allowlist::{
            publisher::remove::RemoveMulticastGroupPubAllowlistCommand,
            subscriber::remove::RemoveMulticastGroupSubAllowlistCommand,
        },
        delete::DeleteMulticastGroupCommand,
        get::GetMulticastGroupCommand,
    },
};
use indicatif::{ProgressBar, ProgressStyle};
use std::{io::Write, net::Ipv4Addr, time::Duration};

#[derive(Args, Debug)]
pub struct DeleteMulticastGroupCliCommand {
    /// Multicast group Pubkey to delete
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub pubkey: String,
}

struct RemovalFailure {
    client_ip: Ipv4Addr,
    allowlist_type: &'static str,
    error: String,
}

impl DeleteMulticastGroupCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let pubkey_or_code = self.pubkey;
        let (pubkey, mgroup) = client.get_multicastgroup(GetMulticastGroupCommand {
            pubkey_or_code: pubkey_or_code.clone(),
        })?;

        // Remove the group from all AccessPass allowlists before deleting
        let accesspasses = client.list_accesspass(ListAccessPassCommand {})?;

        // Filter to only AccessPasses that reference this group
        let affected: Vec<_> = accesspasses
            .into_iter()
            .filter(|(_, ap)| {
                ap.mgroup_pub_allowlist.contains(&pubkey)
                    || ap.mgroup_sub_allowlist.contains(&pubkey)
            })
            .collect();

        let mut failures: Vec<RemovalFailure> = Vec::new();
        let mut removed_count = 0;

        if !affected.is_empty() {
            // Initialize spinner
            let spinner = ProgressBar::new(affected.len() as u64);
            spinner.set_style(
                ProgressStyle::default_spinner()
                    .template("{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} {msg}")
                    .expect("Failed to set template")
                    .progress_chars("#>-")
                    .tick_strings(&["-", "\\", "|", "/"]),
            );
            spinner.enable_steady_tick(Duration::from_millis(100));
            spinner.println(format!(
                "Removing multicast group '{}' from {} AccessPass allowlist(s)...",
                mgroup.code,
                affected.len()
            ));

            for (_, accesspass) in affected {
                spinner.set_message(format!("Processing {}", accesspass.client_ip));

                // Try to remove from publisher allowlist
                if accesspass.mgroup_pub_allowlist.contains(&pubkey) {
                    match client.remove_multicastgroup_pub_allowlist(
                        RemoveMulticastGroupPubAllowlistCommand {
                            pubkey_or_code: pubkey_or_code.clone(),
                            client_ip: accesspass.client_ip,
                            user_payer: accesspass.user_payer,
                        },
                    ) {
                        Ok(_) => {
                            spinner.println(format!(
                                "  ✓ Removed {} from publisher allowlist",
                                accesspass.client_ip
                            ));
                            removed_count += 1;
                        }
                        Err(e) => {
                            spinner.println(format!(
                                "  ✗ Failed to remove {} from publisher allowlist: {}",
                                accesspass.client_ip, e
                            ));
                            failures.push(RemovalFailure {
                                client_ip: accesspass.client_ip,
                                allowlist_type: "publisher",
                                error: e.to_string(),
                            });
                        }
                    }
                }

                // Try to remove from subscriber allowlist
                if accesspass.mgroup_sub_allowlist.contains(&pubkey) {
                    match client.remove_multicastgroup_sub_allowlist(
                        RemoveMulticastGroupSubAllowlistCommand {
                            pubkey_or_code: pubkey_or_code.clone(),
                            client_ip: accesspass.client_ip,
                            user_payer: accesspass.user_payer,
                        },
                    ) {
                        Ok(_) => {
                            spinner.println(format!(
                                "  ✓ Removed {} from subscriber allowlist",
                                accesspass.client_ip
                            ));
                            removed_count += 1;
                        }
                        Err(e) => {
                            spinner.println(format!(
                                "  ✗ Failed to remove {} from subscriber allowlist: {}",
                                accesspass.client_ip, e
                            ));
                            failures.push(RemovalFailure {
                                client_ip: accesspass.client_ip,
                                allowlist_type: "subscriber",
                                error: e.to_string(),
                            });
                        }
                    }
                }

                spinner.inc(1);
            }

            spinner.finish_with_message("Done processing AccessPasses");
        }

        // Delete the multicast group
        let signature = client.delete_multicastgroup(DeleteMulticastGroupCommand { pubkey })?;

        // Print summary
        writeln!(out)?;
        writeln!(
            out,
            "✓ Multicast group '{}' deleted successfully",
            mgroup.code
        )?;
        writeln!(out, "  Signature: {signature}")?;

        if removed_count > 0 {
            writeln!(
                out,
                "  Removed from {removed_count} AccessPass allowlist(s)"
            )?;
        }

        if !failures.is_empty() {
            writeln!(out)?;
            writeln!(
                out,
                "⚠ Warning: Failed to remove group from {} AccessPass allowlist(s):",
                failures.len()
            )?;
            for failure in &failures {
                writeln!(
                    out,
                    "  - {} ({}): {}",
                    failure.client_ip, failure.allowlist_type, failure.error
                )?;
            }
            writeln!(out)?;
            writeln!(
                out,
                "These AccessPasses may contain stale references to the deleted group."
            )?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        multicastgroup::delete::DeleteMulticastGroupCliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
    };
    use doublezero_sdk::{
        commands::multicastgroup::{
            allowlist::{
                publisher::remove::RemoveMulticastGroupPubAllowlistCommand,
                subscriber::remove::RemoveMulticastGroupSubAllowlistCommand,
            },
            delete::DeleteMulticastGroupCommand,
            get::GetMulticastGroupCommand,
        },
        get_multicastgroup_pda, AccountType, MulticastGroup, MulticastGroupStatus,
    };
    use doublezero_serviceability::state::accesspass::{
        AccessPass, AccessPassStatus, AccessPassType,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};
    use std::collections::HashMap;

    #[test]
    fn test_cli_multicastgroup_delete() {
        let mut client = create_test_client();

        let (mgroup_pubkey, _bump_seed) = get_multicastgroup_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let multicastgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            code: "testgroup".to_string(),
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 0, 0, 1].into(),
            max_bandwidth: 1000000000,
            status: MulticastGroupStatus::Activated,
            owner: mgroup_pubkey,
            publisher_count: 1,
            subscriber_count: 2,
        };

        // AccessPass with group in publisher allowlist
        let publisher_ip: std::net::Ipv4Addr = [100, 0, 0, 1].into();
        let publisher_payer = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo1");
        let accesspass_publisher = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 1,
            client_ip: publisher_ip,
            accesspass_type: AccessPassType::Prepaid,
            last_access_epoch: u64::MAX,
            user_payer: publisher_payer,
            mgroup_pub_allowlist: vec![mgroup_pubkey], // Has the group as publisher
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            connection_count: 0,
            status: AccessPassStatus::Requested,
            flags: 0,
        };

        // AccessPass with group in subscriber allowlist
        let subscriber_ip: std::net::Ipv4Addr = [100, 0, 0, 2].into();
        let subscriber_payer = Pubkey::from_str_const("11111115q4EpJaTXAZWpCg3J2zppWGSZ46KXozzo2");
        let accesspass_subscriber = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 1,
            client_ip: subscriber_ip,
            accesspass_type: AccessPassType::Prepaid,
            last_access_epoch: u64::MAX,
            user_payer: subscriber_payer,
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![mgroup_pubkey], // Has the group as subscriber
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            connection_count: 0,
            status: AccessPassStatus::Requested,
            flags: 0,
        };

        // AccessPass with no reference to the group (should not trigger remove)
        let accesspass_unrelated = AccessPass {
            account_type: AccountType::AccessPass,
            bump_seed: 1,
            client_ip: [100, 0, 0, 3].into(),
            accesspass_type: AccessPassType::Prepaid,
            last_access_epoch: u64::MAX,
            user_payer: Pubkey::new_unique(),
            mgroup_pub_allowlist: vec![],
            mgroup_sub_allowlist: vec![],
            tenant_allowlist: vec![],
            owner: Pubkey::new_unique(),
            connection_count: 0,
            status: AccessPassStatus::Requested,
            flags: 0,
        };

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));

        client
            .expect_get_multicastgroup()
            .with(predicate::eq(GetMulticastGroupCommand {
                pubkey_or_code: mgroup_pubkey.to_string(),
            }))
            .returning(move |_| Ok((mgroup_pubkey, multicastgroup.clone())));

        // Return AccessPasses - some with group references, one without
        client.expect_list_accesspass().returning(move |_| {
            let mut list: HashMap<Pubkey, AccessPass> = HashMap::new();
            list.insert(Pubkey::new_unique(), accesspass_publisher.clone());
            list.insert(Pubkey::new_unique(), accesspass_subscriber.clone());
            list.insert(Pubkey::new_unique(), accesspass_unrelated.clone());
            Ok(list)
        });

        // Expect remove from publisher allowlist to be called (for accesspass_publisher)
        client
            .expect_remove_multicastgroup_pub_allowlist()
            .with(predicate::eq(RemoveMulticastGroupPubAllowlistCommand {
                pubkey_or_code: mgroup_pubkey.to_string(),
                client_ip: publisher_ip,
                user_payer: publisher_payer,
            }))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        // Expect remove from subscriber allowlist to be called (for accesspass_subscriber)
        client
            .expect_remove_multicastgroup_sub_allowlist()
            .with(predicate::eq(RemoveMulticastGroupSubAllowlistCommand {
                pubkey_or_code: mgroup_pubkey.to_string(),
                client_ip: subscriber_ip,
                user_payer: subscriber_payer,
            }))
            .times(1)
            .returning(|_| Ok(Signature::new_unique()));

        // Finally expect the group to be deleted
        client
            .expect_delete_multicastgroup()
            .with(predicate::eq(DeleteMulticastGroupCommand {
                pubkey: mgroup_pubkey,
            }))
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = DeleteMulticastGroupCliCommand {
            pubkey: mgroup_pubkey.to_string(),
        }
        .execute(&client, &mut output);
        assert!(res.is_ok(), "Expected delete to succeed: {:?}", res);

        // Verify output contains the expected messages
        let output_str = String::from_utf8(output).unwrap();
        assert!(
            output_str.contains("Multicast group 'testgroup' deleted successfully"),
            "Expected success message in output: {output_str}"
        );
        assert!(
            output_str.contains("Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv"),
            "Expected signature in output: {output_str}"
        );
        assert!(
            output_str.contains("Removed from 2 AccessPass allowlist(s)"),
            "Expected removal count in output: {output_str}"
        );
        // Should NOT contain warning since all removals succeeded
        assert!(
            !output_str.contains("Warning"),
            "Should not have warnings when all removals succeed: {output_str}"
        );
    }
}
