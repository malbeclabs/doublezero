use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_sdk::commands::link::latency::LatencyLinkCommand;
use std::io::Write;
use tabled::{
    settings::{object::Columns, Remove, Style},
    Table, Tabled,
};

#[derive(Tabled)]
pub struct LatencyStatsRow {
    #[tabled(rename = "Link")]
    pub link_code: String,
    #[tabled(rename = "Epoch")]
    pub epoch: u64,
    #[tabled(rename = "Samples")]
    pub samples: usize,
    #[tabled(rename = "P50 (ms)")]
    pub p50: String,
    #[tabled(rename = "P90 (ms)")]
    pub p90: String,
    #[tabled(rename = "P95 (ms)")]
    pub p95: String,
    #[tabled(rename = "P99 (ms)")]
    pub p99: String,
    #[tabled(rename = "Mean (ms)")]
    pub mean: String,
    #[tabled(rename = "Min (ms)")]
    pub min: String,
    #[tabled(rename = "Max (ms)")]
    pub max: String,
    #[tabled(rename = "StdDev (ms)")]
    pub stddev: String,
}

#[derive(Args, Debug)]
pub struct LinkLatencyCliCommand {
    /// The pubkey or code of the link to retrieve
    #[arg(long, value_parser = validate_code)]
    pub code: Option<String>,

    // Possible statistics to display (p50, p90, p95, p99, mean, min, max, stddev, all)
    #[arg(long, default_value = "all")]
    pub p: String,

    // Epoch to query
    #[arg(long)]
    pub epoch: Option<u64>,
}

impl LinkLatencyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let env = client.get_environment();
        let config = env.config()?;

        // Call the SDK command which handles both single and all links
        let stats_vec = client.latency_link(LatencyLinkCommand {
            pubkey_or_code: self.code.clone(),
            epoch: self.epoch,
            telemetry_program_id: config.telemetry_program_id,
        })?;

        // Convert to table rows
        let rows: Vec<LatencyStatsRow> = stats_vec
            .into_iter()
            .map(|stats| LatencyStatsRow {
                link_code: stats
                    .link_code
                    .unwrap_or_else(|| format!("{}", stats.link_pk)),
                epoch: stats.epoch,
                samples: stats.sample_count,
                p50: format!("{:.2}", stats.p50),
                p90: format!("{:.2}", stats.p90),
                p95: format!("{:.2}", stats.p95),
                p99: format!("{:.2}", stats.p99),
                mean: format!("{:.2}", stats.mean),
                min: format!("{:.2}", stats.min),
                max: format!("{:.2}", stats.max),
                stddev: format!("{:.2}", stats.stddev),
            })
            .collect();

        // Build and display table based on --p parameter
        if rows.is_empty() {
            writeln!(out, "No latency data found")?;
        } else {
            let mut table = Table::new(rows).with(Style::psql()).to_owned();

            // Column indices in LatencyStatsRow
            // 0 Link, 1 Epoch, 2 Samples, 3 P50, 4 P90, 5 P95, 6 P99, 7 Mean, 8 Min, 9 Max, 10 StdDev
            match self.p.as_str() {
                "all" => {
                    // keep all columns
                }
                "p50" => {
                    for idx in [10, 9, 8, 7, 6, 5, 4] {
                        table.with(Remove::column(Columns::new(idx..=idx)));
                    }
                }
                "p90" => {
                    for idx in [10, 9, 8, 7, 6, 5, 3] {
                        table.with(Remove::column(Columns::new(idx..=idx)));
                    }
                }
                "p95" => {
                    for idx in [10, 9, 8, 7, 6, 4, 3] {
                        table.with(Remove::column(Columns::new(idx..=idx)));
                    }
                }
                "p99" => {
                    for idx in [10, 9, 8, 7, 5, 4, 3] {
                        table.with(Remove::column(Columns::new(idx..=idx)));
                    }
                }
                "mean" => {
                    for idx in [10, 9, 8, 6, 5, 4, 3] {
                        table.with(Remove::column(Columns::new(idx..=idx)));
                    }
                }
                "min" => {
                    for idx in [10, 9, 7, 6, 5, 4, 3] {
                        table.with(Remove::column(Columns::new(idx..=idx)));
                    }
                }
                "max" => {
                    for idx in [10, 8, 7, 6, 5, 4, 3] {
                        table.with(Remove::column(Columns::new(idx..=idx)));
                    }
                }
                "stddev" => {
                    for idx in [9, 8, 7, 6, 5, 4, 3] {
                        table.with(Remove::column(Columns::new(idx..=idx)));
                    }
                }
                invalid => {
                    eyre::bail!(
                        "Invalid percentile '{}'. Valid options: p50, p90, p95, p99, mean, min, max, stddev, all",
                        invalid
                    );
                }
            }

            writeln!(out, "{}", table)?;
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand, link::latency::LinkLatencyCliCommand,
        tests::utils::create_test_client,
    };
    use doublezero_config::Environment;
    use doublezero_sdk::{
        commands::link::{get::GetLinkCommand, latency::LatencyLinkCommand},
        get_link_pda,
        telemetry::LinkLatencyStats,
        AccountType, Link, LinkLinkType, LinkStatus,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    fn create_test_link(pda_pubkey: Pubkey) -> Link {
        let contributor_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcx");
        let device1_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device2_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf");

        Link {
            account_type: AccountType::Link,
            index: 1,
            bump_seed: 255,
            code: "nyc-lax".to_string(),
            contributor_pk,
            side_a_pk: device1_pk,
            side_z_pk: device2_pk,
            link_type: LinkLinkType::WAN,
            bandwidth: 1000000000,
            mtu: 1500,
            delay_ns: 10000000000,
            jitter_ns: 5000000000,
            tunnel_id: 1,
            tunnel_net: "10.0.0.1/16".parse().unwrap(),
            status: LinkStatus::Activated,
            owner: pda_pubkey,
            side_a_iface_name: "eth0".to_string(),
            side_z_iface_name: "eth1".to_string(),
            delay_override_ns: 0,
        }
    }

    fn create_test_stats(
        link_pk: Pubkey,
        device1_pk: Pubkey,
        device2_pk: Pubkey,
    ) -> LinkLatencyStats {
        LinkLatencyStats {
            epoch: 19800,
            link_pk,
            link_code: Some("nyc-lax".to_string()),
            origin_device_pk: device1_pk,
            target_device_pk: device2_pk,
            sample_count: 1000,
            p50: 12.34, // milliseconds
            p90: 23.45,
            p95: 34.56,
            p99: 45.67,
            mean: 15.23,
            min: 8.12,
            max: 67.89,
            stddev: 5.43,
        }
    }

    #[test]
    fn test_cli_link_latency_p99() {
        let mut client = create_test_client();
        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let link = create_test_link(pda_pubkey);
        let stats = create_test_stats(pda_pubkey, link.side_a_pk, link.side_z_pk);

        let env = Environment::Devnet;
        let telemetry_program_id = env.config().unwrap().telemetry_program_id;

        client.expect_get_environment().returning(move || env);

        client
            .expect_get_link()
            .with(predicate::eq(GetLinkCommand {
                pubkey_or_code: "nyc-lax".to_string(),
            }))
            .returning(move |_| Ok((pda_pubkey, link.clone())));

        client
            .expect_latency_link()
            .with(predicate::function(move |cmd: &LatencyLinkCommand| {
                cmd.pubkey_or_code == Some("nyc-lax".to_string())
                    && cmd.epoch.is_none()
                    && cmd.telemetry_program_id == telemetry_program_id
            }))
            .returning(move |_| Ok(vec![stats.clone()]));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: Some("nyc-lax".to_string()),
            p: "p99".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed");
        let output_str = String::from_utf8(output).unwrap();

        // Verify we ONLY see P99, not other metrics
        assert!(output_str.contains("Link"), "Should have Link header");
        assert!(output_str.contains("Epoch"), "Should have Epoch header");
        assert!(output_str.contains("Samples"), "Should have Samples header");
        assert!(output_str.contains("P99 (ms)"), "Should have P99 column");

        // Verify other metric columns are NOT present
        assert!(
            !output_str.contains("P50 (ms)"),
            "Should NOT have P50 column"
        );
        assert!(
            !output_str.contains("P90 (ms)"),
            "Should NOT have P90 column"
        );
        assert!(
            !output_str.contains("P95 (ms)"),
            "Should NOT have P95 column"
        );
        assert!(
            !output_str.contains("Mean (ms)"),
            "Should NOT have Mean column"
        );
        assert!(
            !output_str.contains("StdDev (ms)"),
            "Should NOT have StdDev column"
        );

        // Verify data
        assert!(output_str.contains("nyc-lax"), "Should contain link code");
        assert!(output_str.contains("45.67"), "Should contain p99 value");
        assert!(output_str.contains("19800"), "Should contain epoch");
    }

    #[test]
    fn test_cli_link_latency_p50() {
        let mut client = create_test_client();
        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let link = create_test_link(pda_pubkey);
        let stats = create_test_stats(pda_pubkey, link.side_a_pk, link.side_z_pk);

        let env = Environment::Devnet;

        client.expect_get_environment().returning(move || env);

        client
            .expect_get_link()
            .returning(move |_| Ok((pda_pubkey, link.clone())));

        client
            .expect_latency_link()
            .returning(move |_| Ok(vec![stats.clone()]));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: Some("nyc-lax".to_string()),
            p: "p50".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed");
        let output_str = String::from_utf8(output).unwrap();

        // Verify we ONLY see P50, not other metrics
        assert!(output_str.contains("Link"), "Should have Link header");
        assert!(output_str.contains("Epoch"), "Should have Epoch header");
        assert!(output_str.contains("Samples"), "Should have Samples header");
        assert!(output_str.contains("P50 (ms)"), "Should have P50 column");

        // Verify other metric columns are NOT present
        assert!(
            !output_str.contains("P90 (ms)"),
            "Should NOT have P90 column"
        );
        assert!(
            !output_str.contains("P95 (ms)"),
            "Should NOT have P95 column"
        );
        assert!(
            !output_str.contains("P99 (ms)"),
            "Should NOT have P99 column"
        );
        assert!(
            !output_str.contains("Mean (ms)"),
            "Should NOT have Mean column"
        );
        assert!(
            !output_str.contains("StdDev (ms)"),
            "Should NOT have StdDev column"
        );

        // Verify data
        assert!(output_str.contains("nyc-lax"), "Should contain link code");
        assert!(output_str.contains("12.34"), "Should contain p50 value");
        assert!(output_str.contains("19800"), "Should contain epoch");
    }

    #[test]
    fn test_cli_link_latency_mean() {
        let mut client = create_test_client();
        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let link = create_test_link(pda_pubkey);
        let stats = create_test_stats(pda_pubkey, link.side_a_pk, link.side_z_pk);

        let env = Environment::Devnet;

        client.expect_get_environment().returning(move || env);

        client
            .expect_get_link()
            .returning(move |_| Ok((pda_pubkey, link.clone())));

        client
            .expect_latency_link()
            .returning(move |_| Ok(vec![stats.clone()]));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: Some("nyc-lax".to_string()),
            p: "mean".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed");
        let output_str = String::from_utf8(output).unwrap();

        // Verify we ONLY see Mean, not other metrics
        assert!(output_str.contains("Link"), "Should have Link header");
        assert!(output_str.contains("Epoch"), "Should have Epoch header");
        assert!(output_str.contains("Samples"), "Should have Samples header");
        assert!(output_str.contains("Mean (ms)"), "Should have Mean column");

        // Verify other metric columns are NOT present
        assert!(
            !output_str.contains("P50 (ms)"),
            "Should NOT have P50 column"
        );
        assert!(
            !output_str.contains("P90 (ms)"),
            "Should NOT have P90 column"
        );
        assert!(
            !output_str.contains("P95 (ms)"),
            "Should NOT have P95 column"
        );
        assert!(
            !output_str.contains("P99 (ms)"),
            "Should NOT have P99 column"
        );
        assert!(
            !output_str.contains("StdDev (ms)"),
            "Should NOT have StdDev column"
        );

        // Verify data
        assert!(output_str.contains("nyc-lax"), "Should contain link code");
        assert!(output_str.contains("15.23"), "Should contain mean value");
        assert!(output_str.contains("19800"), "Should contain epoch");
    }

    #[test]
    fn test_cli_link_latency_all() {
        let mut client = create_test_client();
        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let link = create_test_link(pda_pubkey);
        let stats = create_test_stats(pda_pubkey, link.side_a_pk, link.side_z_pk);

        let env = Environment::Devnet;

        client.expect_get_environment().returning(move || env);

        client
            .expect_get_link()
            .returning(move |_| Ok((pda_pubkey, link.clone())));

        client
            .expect_latency_link()
            .returning(move |_| Ok(vec![stats.clone()]));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: Some("nyc-lax".to_string()),
            p: "all".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed");
        let output_str = String::from_utf8(output).unwrap();

        // Verify table headers
        assert!(output_str.contains("Link"), "Should have Link header");
        assert!(output_str.contains("Epoch"), "Should have Epoch header");
        assert!(output_str.contains("Samples"), "Should have Samples header");
        assert!(output_str.contains("P50 (ms)"), "Should have P50 header");
        assert!(output_str.contains("P99 (ms)"), "Should have P99 header");
        assert!(output_str.contains("Mean (ms)"), "Should have Mean header");

        // Verify data
        assert!(output_str.contains("nyc-lax"), "Should contain link code");
        assert!(output_str.contains("19800"), "Should contain epoch");
        assert!(output_str.contains("1000"), "Should contain samples");
        assert!(output_str.contains("12.34"), "Should contain p50");
        assert!(output_str.contains("45.67"), "Should contain p99");
        assert!(output_str.contains("15.23"), "Should contain mean");
    }

    #[test]
    fn test_cli_link_latency_with_epoch() {
        let mut client = create_test_client();
        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let link = create_test_link(pda_pubkey);
        let stats = create_test_stats(pda_pubkey, link.side_a_pk, link.side_z_pk);

        let env = Environment::Devnet;
        let telemetry_program_id = env.config().unwrap().telemetry_program_id;

        client.expect_get_environment().returning(move || env);

        client
            .expect_get_link()
            .returning(move |_| Ok((pda_pubkey, link.clone())));

        client
            .expect_latency_link()
            .with(predicate::function(move |cmd: &LatencyLinkCommand| {
                cmd.pubkey_or_code == Some("nyc-lax".to_string())
                    && cmd.epoch == Some(12345)
                    && cmd.telemetry_program_id == telemetry_program_id
            }))
            .returning(move |_| Ok(vec![stats.clone()]));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: Some("nyc-lax".to_string()),
            p: "p99".to_string(),
            epoch: Some(12345),
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed with epoch");
        let output_str = String::from_utf8(output).unwrap();

        // Verify we ONLY see P99, not other metrics
        assert!(output_str.contains("Link"), "Should have Link header");
        assert!(output_str.contains("Epoch"), "Should have Epoch header");
        assert!(output_str.contains("Samples"), "Should have Samples header");
        assert!(output_str.contains("P99 (ms)"), "Should have P99 column");

        // Verify other metric columns are NOT present
        assert!(
            !output_str.contains("P50 (ms)"),
            "Should NOT have P50 column"
        );
        assert!(
            !output_str.contains("Mean (ms)"),
            "Should NOT have Mean column"
        );

        // Verify data
        assert!(output_str.contains("nyc-lax"), "Should contain link code");
        assert!(output_str.contains("45.67"), "Should contain p99 value");
    }

    #[test]
    fn test_cli_link_latency_link_not_found() {
        let mut client = create_test_client();

        let env = Environment::Devnet;

        client.expect_get_environment().returning(move || env);

        client
            .expect_latency_link()
            .returning(|_| Err(eyre::eyre!("Link not found")));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: Some("nonexistent".to_string()),
            p: "p99".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_err(), "Should fail when link not found");
    }

    #[test]
    fn test_cli_link_latency_invalid_percentile() {
        let mut client = create_test_client();
        let (pda_pubkey, _bump_seed) = get_link_pda(&client.get_program_id(), 1);
        let link = create_test_link(pda_pubkey);
        let stats = create_test_stats(pda_pubkey, link.side_a_pk, link.side_z_pk);

        let env = Environment::Devnet;

        client.expect_get_environment().returning(move || env);

        client
            .expect_latency_link()
            .returning(move |_| Ok(vec![stats.clone()]));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: Some("nyc-lax".to_string()),
            p: "invalid".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_err(), "Should fail with invalid percentile");
        let err = res.unwrap_err();
        assert!(
            err.to_string().contains("Invalid percentile"),
            "Error should mention invalid percentile"
        );
        assert!(
            err.to_string().contains("invalid"),
            "Error should include the invalid value"
        );
        assert!(
            err.to_string().contains("p50"),
            "Error should list valid options"
        );
    }

    #[test]
    fn test_cli_link_latency_all_links_multiple() {
        let mut client = create_test_client();

        // Create two different links
        let (pda_pubkey1, _) = get_link_pda(&client.get_program_id(), 1);
        let device1a_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcb");
        let device1z_pk = Pubkey::from_str_const("HQ2UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkcf");

        let (pda_pubkey2, _) = get_link_pda(&client.get_program_id(), 2);
        let device2a_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkca");
        let device2z_pk = Pubkey::from_str_const("HQ3UUt18uJqKaQFJhgV9zaTdQxUZjNrsKFgoEDquBkce");

        // Create stats for both links
        let stats1 = LinkLatencyStats {
            epoch: 19800,
            link_pk: pda_pubkey1,
            link_code: Some("nyc-lax".to_string()),
            origin_device_pk: device1a_pk,
            target_device_pk: device1z_pk,
            sample_count: 1000,
            p50: 12.34,
            p90: 23.45,
            p95: 34.56,
            p99: 45.67,
            mean: 15.23,
            min: 8.12,
            max: 67.89,
            stddev: 5.43,
        };

        let stats2 = LinkLatencyStats {
            epoch: 19800,
            link_pk: pda_pubkey2,
            link_code: Some("sfo-sea".to_string()),
            origin_device_pk: device2a_pk,
            target_device_pk: device2z_pk,
            sample_count: 850,
            p50: 8.21,
            p90: 15.32,
            p95: 18.45,
            p99: 22.11,
            mean: 9.87,
            min: 5.43,
            max: 45.21,
            stddev: 3.21,
        };

        let env = Environment::Devnet;
        let telemetry_program_id = env.config().unwrap().telemetry_program_id;

        client.expect_get_environment().returning(move || env);

        client
            .expect_latency_link()
            .with(predicate::function(move |cmd: &LatencyLinkCommand| {
                cmd.pubkey_or_code.is_none()
                    && cmd.epoch.is_none()
                    && cmd.telemetry_program_id == telemetry_program_id
            }))
            .returning(move |_| Ok(vec![stats1.clone(), stats2.clone()]));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: None, // Query all links
            p: "all".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed");
        let output_str = String::from_utf8(output).unwrap();

        // Verify both links are in the output
        assert!(output_str.contains("nyc-lax"), "Should contain first link");
        assert!(output_str.contains("sfo-sea"), "Should contain second link");

        // Verify first link stats
        assert!(output_str.contains("1000")); // samples for nyc-lax
        assert!(output_str.contains("12.34")); // p50 for nyc-lax
        assert!(output_str.contains("45.67")); // p99 for nyc-lax

        // Verify second link stats
        assert!(output_str.contains("850")); // samples for sfo-sea
        assert!(output_str.contains("8.21")); // p50 for sfo-sea
        assert!(output_str.contains("22.11")); // p99 for sfo-sea

        // Verify it's a table format
        assert!(output_str.contains("Link")); // Table header
        assert!(output_str.contains("Epoch"));
        assert!(output_str.contains("Samples"));
    }
}
