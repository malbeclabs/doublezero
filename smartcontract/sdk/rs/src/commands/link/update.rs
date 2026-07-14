use crate::{
    commands::{
        contributor::get::GetContributorCommand, device::get::GetDeviceCommand,
        link::get::GetLinkCommand,
    },
    DoubleZeroClient,
};
use doublezero_program_common::{types::NetworkV4, validate_account_code};
use doublezero_serviceability::{
    processors::link::update::LinkUpdateArgs,
    state::link::{LinkDesiredStatus, LinkLinkType, LinkStatus},
};
use doublezero_serviceability_instruction::link::{update_link, LinkUpdateAuthority};
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct UpdateLinkCommand {
    pub pubkey: Pubkey,
    pub code: Option<String>,
    pub contributor_pk: Option<Pubkey>,
    pub tunnel_type: Option<LinkLinkType>,
    pub bandwidth: Option<u64>,
    pub mtu: Option<u32>,
    pub delay_ns: Option<u64>,
    pub jitter_ns: Option<u64>,
    pub delay_override_ns: Option<u64>,
    pub status: Option<LinkStatus>,
    pub desired_status: Option<LinkDesiredStatus>,
    pub tunnel_id: Option<u16>,
    pub tunnel_net: Option<NetworkV4>,
    pub link_topologies: Option<Vec<Pubkey>>,
    pub unicast_drained: Option<bool>,
}

impl UpdateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<Signature> {
        let (_, link) = GetLinkCommand {
            pubkey_or_code: self.pubkey.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Link not found"))?;

        // Fetch to check if side Z is the caller
        let payer = client.get_payer();

        let (_, device_z) = GetDeviceCommand {
            pubkey_or_code: link.side_z_pk.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Device Z not found"))?;

        let (_, contributor_z) = GetContributorCommand {
            pubkey_or_code: device_z.contributor_pk.to_string(),
        }
        .execute(client)
        .map_err(|_err| eyre::eyre!("Contributor Z not found"))?;

        let code = self
            .code
            .as_ref()
            .map(|code| {
                validate_account_code(code).map(|mut c| {
                    c.make_ascii_lowercase();
                    c
                })
            })
            .transpose()
            .map_err(|err| eyre::eyre!("invalid code: {err}"))?;

        // The link's own contributor authorizes, unless side-Z's contributor is the
        // caller (owned by payer), in which case the side-Z preamble is used.
        let authority = if contributor_z.owner == payer {
            LinkUpdateAuthority::SideZContributor {
                contributor: &device_z.contributor_pk,
            }
        } else {
            LinkUpdateAuthority::Contributor {
                contributor: &link.contributor_pk,
            }
        };

        // When updating link_topologies, the processor diffs old vs new on-chain and
        // adjusts each topology's reference_count. Pass the union of the Link's current
        // link_topologies and the requested new set, all writable.
        let topology_union: Vec<Pubkey> = if let Some(ref new_topologies) = self.link_topologies {
            let mut union: Vec<Pubkey> = link.link_topologies.clone();
            for pk in new_topologies {
                if !union.contains(pk) {
                    union.push(*pk);
                }
            }
            union
        } else {
            vec![]
        };

        client.send_transaction(update_link(
            &client.get_program_id(),
            &payer,
            &self.pubkey,
            authority,
            &link.side_a_pk,
            &link.side_z_pk,
            &topology_union,
            LinkUpdateArgs {
                code,
                contributor_pk: self.contributor_pk,
                tunnel_type: self.tunnel_type,
                bandwidth: self.bandwidth,
                mtu: self.mtu,
                delay_ns: self.delay_ns,
                jitter_ns: self.jitter_ns,
                delay_override_ns: self.delay_override_ns,
                status: self.status,
                desired_status: self.desired_status,
                tunnel_id: self.tunnel_id,
                tunnel_net: self.tunnel_net,
                use_onchain_allocation: false,
                link_topologies: self.link_topologies.clone(),
                unicast_drained: self.unicast_drained,
            },
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{tests::utils::create_test_client, MockDoubleZeroClient};
    use doublezero_serviceability::state::{
        accountdata::AccountData,
        accounttype::AccountType,
        contributor::{Contributor, ContributorStatus},
        device::Device,
        link::Link,
    };
    use mockall::predicate;

    fn make_contributor(owner: Pubkey, code: &str) -> Contributor {
        Contributor {
            account_type: AccountType::Contributor,
            owner,
            index: 1,
            bump_seed: 0,
            status: ContributorStatus::Activated,
            code: code.to_string(),
            reference_count: 0,
            ops_manager_pk: Pubkey::default(),
        }
    }

    /// Builds and wires the get() mocks for `link`, `side_z` device, and `side_z`'s contributor.
    /// `contributor_z_owner` controls whether the side-Z path is selected.
    /// Returns `(link_pubkey, contributor_a_pk, contributor_z_pk, side_z_pk)`.
    fn setup_link_and_contributors(
        client: &mut MockDoubleZeroClient,
        contributor_z_owner: Pubkey,
    ) -> (Pubkey, Pubkey, Pubkey, Pubkey) {
        let link_pubkey = Pubkey::new_unique();
        let contributor_a_pk = Pubkey::new_unique();
        let contributor_z_pk = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();

        let link = Link {
            contributor_pk: contributor_a_pk,
            side_a_pk,
            side_z_pk,
            ..Default::default()
        };
        let device_z = Device {
            contributor_pk: contributor_z_pk,
            ..Default::default()
        };
        let contributor_z = make_contributor(contributor_z_owner, "co_z");

        client
            .expect_get()
            .with(predicate::eq(link_pubkey))
            .returning(move |_| Ok(AccountData::Link(link.clone())));
        client
            .expect_get()
            .with(predicate::eq(side_z_pk))
            .returning(move |_| Ok(AccountData::Device(device_z.clone())));
        client
            .expect_get()
            .with(predicate::eq(contributor_z_pk))
            .returning(move |_| Ok(AccountData::Contributor(contributor_z.clone())));

        (link_pubkey, contributor_a_pk, contributor_z_pk, side_z_pk)
    }

    fn drain_command(link_pubkey: Pubkey) -> UpdateLinkCommand {
        UpdateLinkCommand {
            pubkey: link_pubkey,
            code: None,
            contributor_pk: None,
            tunnel_type: None,
            bandwidth: None,
            mtu: None,
            delay_ns: None,
            jitter_ns: None,
            delay_override_ns: None,
            status: Some(LinkStatus::SoftDrained),
            desired_status: None,
            tunnel_id: None,
            tunnel_net: None,
            link_topologies: None,
            unicast_drained: None,
        }
    }

    #[test]
    fn test_update_link_side_z_uses_4_account_preamble() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();

        // contributor_z.owner == payer  =>  SDK should pick the side-Z layout
        let (link_pubkey, _contributor_a_pk, contributor_z_pk, side_z_pk) =
            setup_link_and_contributors(&mut client, payer);

        // No tunnel_net / topologies -> side_a/side_z are not appended, so their
        // values don't affect the produced instruction.
        let expected = update_link(
            &program_id,
            &payer,
            &link_pubkey,
            LinkUpdateAuthority::SideZContributor {
                contributor: &contributor_z_pk,
            },
            &Pubkey::default(),
            &side_z_pk,
            &[],
            LinkUpdateArgs {
                status: Some(LinkStatus::SoftDrained),
                ..Default::default()
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = drain_command(link_pubkey).execute(&client);
        assert!(res.is_ok(), "execute failed: {:?}", res);
    }

    #[test]
    fn test_update_link_side_a_uses_3_account_preamble() {
        let mut client = create_test_client();
        let program_id = client.get_program_id();
        let payer = client.get_payer();

        // contributor_z.owner != payer  =>  SDK should fall back to the side-A layout
        let (link_pubkey, contributor_a_pk, _contributor_z_pk, side_z_pk) =
            setup_link_and_contributors(&mut client, Pubkey::new_unique());

        let expected = update_link(
            &program_id,
            &payer,
            &link_pubkey,
            LinkUpdateAuthority::Contributor {
                contributor: &contributor_a_pk,
            },
            &Pubkey::default(),
            &side_z_pk,
            &[],
            LinkUpdateArgs {
                status: Some(LinkStatus::SoftDrained),
                ..Default::default()
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = drain_command(link_pubkey).execute(&client);
        assert!(res.is_ok(), "execute failed: {:?}", res);
    }
}
