use crate::{doublezerocommand::CliCommand, validators::validate_code};
use clap::Args;
use doublezero_sdk::commands::link::latency::LatencyLinkCommand;
use std::io::Write;

#[derive(Args, Debug)]
pub struct LinkLatencyCliCommand {
    /// The pubkey or code of the link to retrieve
    #[arg(long, value_parser = validate_code)]
    pub code: String,

    // Possible statistics to display (p50, p90, p95, p99, mean, min, max, stddev, all)
    #[arg(long, default_value = "p99")]
    pub p: String,

    // Epoch to query
    #[arg(long)]
    pub epoch: Option<u64>,
}

impl LinkLatencyCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let env = client.get_environment();
        let config = env.config()?;

        // Call sdk command
        let stats = client.latency_link(LatencyLinkCommand {
            pubkey_or_code: self.code,
            epoch: self.epoch,
            telemetry_program_id: config.telemetry_program_id,
        })?;

        // Display output based on -p (percentile) flag
        match self.p.to_lowercase().as_str() {
            "p50" => writeln!(out, "P50: {:.2}ms", stats.p50)?,
            "p90" => writeln!(out, "P90: {:.2}ms", stats.p90)?,
            "p95" => writeln!(out, "P95: {:.2}ms", stats.p95)?,
            "p99" => writeln!(out, "P99: {:.2}ms", stats.p99)?,
            "mean" => writeln!(out, "Mean: {:.2}ms", stats.mean)?,
            "min" => writeln!(out, "Min: {:.2}ms", stats.min)?,
            "max" => writeln!(out, "Max: {:.2}ms", stats.max)?,
            "stddev" => writeln!(out, "StdDev: {:.2}ms", stats.stddev)?,
            "all" => {
                writeln!(out, "Link Latency Statistics (Epoch: {})", stats.epoch)?;
                writeln!(out, "Sample Count: {}", stats.sample_count)?;
                writeln!(out, "P50: {:.2}ms", stats.p50)?;
                writeln!(out, "P90: {:.2}ms", stats.p90)?;
                writeln!(out, "P95: {:.2}ms", stats.p95)?;
                writeln!(out, "P99: {:.2}ms", stats.p99)?;
                writeln!(out, "Mean: {:.2}ms", stats.mean)?;
                writeln!(out, "Min: {:.2}ms", stats.min)?;
                writeln!(out, "Max: {:.2}ms", stats.max)?;
                writeln!(out, "StdDev: {:.2}ms", stats.stddev)?;
            }
            _ => {
                eyre::bail!(
                    "Invalid percentile '{}'. Valid options: p50, p90, p95, p99, mean, min, max, stddev, all",
                    self.p
                );
            }
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
                cmd.pubkey_or_code == "nyc-lax"
                    && cmd.epoch.is_none()
                    && cmd.telemetry_program_id == telemetry_program_id
            }))
            .returning(move |_| Ok(stats.clone()));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: "nyc-lax".to_string(),
            p: "p99".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "P99: 45.67ms\n");
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
            .returning(move |_| Ok(stats.clone()));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: "nyc-lax".to_string(),
            p: "p50".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "P50: 12.34ms\n");
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
            .returning(move |_| Ok(stats.clone()));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: "nyc-lax".to_string(),
            p: "mean".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "Mean: 15.23ms\n");
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
            .returning(move |_| Ok(stats.clone()));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: "nyc-lax".to_string(),
            p: "all".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed");
        let output_str = String::from_utf8(output).unwrap();

        // Verify key parts of the output
        assert!(output_str.contains("Link Latency Statistics"));
        assert!(output_str.contains("Epoch: 19800"));
        assert!(output_str.contains("Sample Count: 1000"));
        assert!(output_str.contains("P50: 12.34ms"));
        assert!(output_str.contains("P99: 45.67ms"));
        assert!(output_str.contains("Mean: 15.23ms"));
    }

    #[test]
    fn test_cli_link_latency_invalid_stat() {
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
            .returning(move |_| Ok(stats.clone()));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: "nyc-lax".to_string(),
            p: "invalid".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_err(), "Should fail with invalid stat");
        let err = res.unwrap_err();
        assert!(err.to_string().contains("Invalid percentile"));
        assert!(err.to_string().contains("invalid"));
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
                cmd.pubkey_or_code == "nyc-lax"
                    && cmd.epoch == Some(12345)
                    && cmd.telemetry_program_id == telemetry_program_id
            }))
            .returning(move |_| Ok(stats.clone()));

        let mut output = Vec::new();
        let res = LinkLatencyCliCommand {
            code: "nyc-lax".to_string(),
            p: "p99".to_string(),
            epoch: Some(12345),
        }
        .execute(&client, &mut output);

        assert!(res.is_ok(), "Should succeed with epoch");
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(output_str, "P99: 45.67ms\n");
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
            code: "nonexistent".to_string(),
            p: "p99".to_string(),
            epoch: None,
        }
        .execute(&client, &mut output);

        assert!(res.is_err(), "Should fail when link not found");
    }
}
