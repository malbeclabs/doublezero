use std::io::Write;

use doublezero_ledger_sentinel::{
    client::solana::SolRpcClientType, constants::ENV_PREVIOUS_LEADER_EPOCHS,
};
use doublezero_solana_client_tools::rpc::SolanaConnection;
use solana_client::rpc_response::RpcContactInfo;
use solana_sdk::pubkey::Pubkey;

use crate::{
    error::{PassportCliError, Result},
    util::find_node_by_node_id,
};

pub async fn validate_validator_access<C, W>(
    out: &mut W,
    connection: &SolanaConnection,
    sol_client: &C,
    primary_validator_id: &Pubkey,
    backup_validator_ids: &[Pubkey],
    leader_schedule_epochs: Option<u8>,
) -> Result<Vec<String>>
where
    C: SolRpcClientType + Sync,
    W: Write,
{
    let nodes = connection.get_cluster_nodes().await?;
    if nodes.is_empty() {
        return Err(PassportCliError::ClusterNodesUnavailable);
    }

    validate_validator_access_with_nodes(
        out,
        &nodes,
        sol_client,
        primary_validator_id,
        backup_validator_ids,
        leader_schedule_epochs,
    )
    .await
}

pub async fn validate_validator_access_with_nodes<C, W>(
    out: &mut W,
    nodes: &[RpcContactInfo],
    sol_client: &C,
    primary_validator_id: &Pubkey,
    backup_validator_ids: &[Pubkey],
    leader_schedule_epochs: Option<u8>,
) -> Result<Vec<String>>
where
    C: SolRpcClientType + Sync,
    W: Write,
{
    let mut errors = Vec::<String>::new();
    let leader_schedule_epochs = leader_schedule_epochs.unwrap_or(ENV_PREVIOUS_LEADER_EPOCHS);

    writeln!(
        out,
        "Primary validator 🖥️  💎:\n  ID: {primary_validator_id} "
    )?;
    if let Some(node) = find_node_by_node_id(nodes, primary_validator_id) {
        writeln!(
            out,
            "  Gossip: ✅ OK ({})",
            node.gossip.as_ref().map(|g| g.ip()).unwrap()
        )?;
        write!(out, "  Leader scheduler: ")?;

        if sol_client
            .is_scheduled_leader(primary_validator_id, leader_schedule_epochs)
            .await?
        {
            write!(out, " ✅ OK ")?;
        } else {
            write!(out, " ❌ Invalid ")?;
            errors.push(format!(
                "Primary validator ID ({primary_validator_id}) is not an active staked validator. The primary must have stake delegated and be participating in the leader scheduler."
            ));
        }
    } else {
        writeln!(out, " ❌ Gossip Fail")?;
        errors.push(format!(
            "Primary validator ID ({primary_validator_id}) is not visible in gossip. The primary validator must appear in gossip to be considered active."
        ));
    }
    writeln!(out)?;

    if !backup_validator_ids.is_empty() {
        writeln!(out, "\nBackup validator 🖥️  🛟: ")?;

        for backup_id in backup_validator_ids {
            write!(out, "  ID: {backup_id}\n  Gossip: ")?;

            if let Some(ip) = sol_client.get_validator_ip(backup_id).await? {
                writeln!(out, " ✅ OK ({ip})")?;
                write!(out, "  Leader scheduler: ")?;

                if sol_client
                    .is_scheduled_leader(backup_id, leader_schedule_epochs)
                    .await?
                {
                    writeln!(out, " ❌ Fail (on leader scheduler)")?;
                    errors.push(format!(
                        "Backup validator ID ({backup_id}) should not be on leader scheduler. It must be a non-leader scheduled validator."
                    ));
                } else {
                    writeln!(out, " ✅ OK (not a leader scheduled validator)")?;
                }
            } else {
                writeln!(out, "❌ Gossip Fail")?;
                errors.push(format!(
                    "Backup validator ID ({backup_id}) is not visible in gossip. Backup validators must appear in gossip to be considered valid."
                ));
            }
        }
    }

    Ok(errors)
}

pub fn should_continue_after_validation<W: Write>(
    out: &mut W,
    errors: &[String],
    force: bool,
) -> Result<bool> {
    if errors.is_empty() {
        return Ok(true);
    }

    writeln!(out, "\nErrors found:")?;
    for error in errors {
        writeln!(out, " - {error}")?;
    }

    if force {
        writeln!(out, "Proceeding despite validation errors (--force).")?;
        Ok(true)
    } else {
        Ok(false)
    }
}

#[cfg(test)]
mod tests {
    use std::net::{Ipv4Addr, SocketAddr};

    use doublezero_ledger_sentinel::{
        client::solana::MockSolRpcClientType, constants::ENV_PREVIOUS_LEADER_EPOCHS,
    };
    use solana_client::rpc_response::RpcContactInfo;
    use solana_sdk::pubkey::Pubkey;

    use super::{should_continue_after_validation, validate_validator_access_with_nodes};

    fn make_contact_info(pubkey: &Pubkey, gossip: Option<SocketAddr>) -> RpcContactInfo {
        RpcContactInfo {
            pubkey: pubkey.to_string(),
            gossip,
            tvu: None,
            tpu: None,
            tpu_quic: None,
            tpu_forwards: None,
            tpu_forwards_quic: None,
            tpu_vote: None,
            serve_repair: None,
            rpc: None,
            pubsub: None,
            version: None,
            feature_set: None,
            shred_version: None,
        }
    }

    #[tokio::test]
    async fn validation_succeeds_with_default_leader_schedule_epochs() {
        let primary = Pubkey::new_unique();
        let backup = Pubkey::new_unique();
        let nodes = vec![
            make_contact_info(
                &primary,
                Some(SocketAddr::from((Ipv4Addr::LOCALHOST, 8001))),
            ),
            make_contact_info(&backup, Some(SocketAddr::from((Ipv4Addr::LOCALHOST, 8002)))),
        ];

        let mut client = MockSolRpcClientType::new();

        {
            let primary_clone = primary;
            client
                .expect_is_scheduled_leader()
                .withf(move |validator_id, epochs| {
                    validator_id == &primary_clone && *epochs == ENV_PREVIOUS_LEADER_EPOCHS
                })
                .returning(|_, _| Ok(true));
        }
        {
            let backup_clone = backup;
            client
                .expect_get_validator_ip()
                .withf(move |validator_id| validator_id == &backup_clone)
                .returning(|_| Ok(Some(Ipv4Addr::LOCALHOST)));
        }
        {
            let backup_clone = backup;
            client
                .expect_is_scheduled_leader()
                .withf(move |validator_id, epochs| {
                    validator_id == &backup_clone && *epochs == ENV_PREVIOUS_LEADER_EPOCHS
                })
                .returning(|_, _| Ok(false));
        }

        let mut out = Vec::new();
        let errors = validate_validator_access_with_nodes(
            &mut out,
            &nodes,
            &client,
            &primary,
            &[backup],
            None,
        )
        .await
        .unwrap();

        assert!(errors.is_empty());
    }

    #[tokio::test]
    async fn validation_fails_for_missing_primary_and_leader_backup() {
        let primary = Pubkey::new_unique();
        let backup = Pubkey::new_unique();
        let nodes = vec![make_contact_info(
            &backup,
            Some(SocketAddr::from((Ipv4Addr::LOCALHOST, 8002))),
        )];

        let mut client = MockSolRpcClientType::new();
        {
            let backup_clone = backup;
            client
                .expect_get_validator_ip()
                .withf(move |validator_id| validator_id == &backup_clone)
                .returning(|_| Ok(Some(Ipv4Addr::LOCALHOST)));
        }
        {
            let backup_clone = backup;
            client
                .expect_is_scheduled_leader()
                .withf(move |validator_id, epochs| {
                    validator_id == &backup_clone && *epochs == ENV_PREVIOUS_LEADER_EPOCHS
                })
                .returning(|_, _| Ok(true));
        }

        let mut out = Vec::new();
        let errors = validate_validator_access_with_nodes(
            &mut out,
            &nodes,
            &client,
            &primary,
            &[backup],
            None,
        )
        .await
        .unwrap();

        assert_eq!(errors.len(), 2);
        assert!(errors.iter().any(|e| e.contains("not visible in gossip")));
        assert!(
            errors
                .iter()
                .any(|e| e.contains("should not be on leader scheduler"))
        );
    }

    #[test]
    fn should_continue_respects_force_flag() {
        let errors = vec!["some error".to_string()];
        let mut out = Vec::new();
        assert!(!should_continue_after_validation(&mut out, &errors, false).unwrap());
        let mut out = Vec::new();
        assert!(should_continue_after_validation(&mut out, &errors, true).unwrap());
    }

    #[tokio::test]
    async fn validation_uses_custom_leader_schedule_epochs() {
        let primary = Pubkey::new_unique();
        let nodes = vec![make_contact_info(
            &primary,
            Some(SocketAddr::from((Ipv4Addr::LOCALHOST, 8001))),
        )];

        let mut client = MockSolRpcClientType::new();
        {
            let primary_clone = primary;
            client
                .expect_is_scheduled_leader()
                .withf(move |validator_id, epochs| validator_id == &primary_clone && *epochs == 1)
                .returning(|_, _| Ok(true));
        }

        let mut out = Vec::new();
        let errors =
            validate_validator_access_with_nodes(&mut out, &nodes, &client, &primary, &[], Some(1))
                .await
                .unwrap();

        assert!(errors.is_empty());
    }
}
