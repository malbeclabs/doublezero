use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    pda::get_link_pda,
    processors::link::create::LinkCreateArgs,
    state::link::{LinkDesiredStatus, LinkLinkType},
};
use doublezero_serviceability_instruction::link::create_link;
use solana_sdk::{pubkey::Pubkey, signature::Signature};

#[derive(Debug, PartialEq, Clone)]
pub struct CreateLinkCommand {
    pub code: String,
    pub contributor_pk: Pubkey,
    pub desired_status: Option<LinkDesiredStatus>,
    pub side_a_pk: Pubkey,
    pub side_z_pk: Pubkey,
    pub link_type: LinkLinkType,
    pub bandwidth: u64,
    pub mtu: u32,
    pub delay_ns: u64,
    pub jitter_ns: u64,
    pub side_a_iface_name: String,
    pub side_z_iface_name: Option<String>,
}

impl CreateLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Signature, Pubkey)> {
        let mut code =
            validate_account_code(&self.code).map_err(|err| eyre::eyre!("invalid code: {err}"))?;
        code.make_ascii_lowercase();

        let (_globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let program_id = client.get_program_id();
        let link_index = globalstate.account_index + 1;
        let (pda_pubkey, _) = get_link_pda(&program_id, link_index);

        let ix = create_link(
            &program_id,
            &client.get_payer(),
            &self.contributor_pk,
            &self.side_a_pk,
            &self.side_z_pk,
            link_index,
            LinkCreateArgs {
                code,
                link_type: self.link_type,
                desired_status: self.desired_status,
                bandwidth: self.bandwidth,
                mtu: self.mtu,
                delay_ns: self.delay_ns,
                jitter_ns: self.jitter_ns,
                side_a_iface_name: self.side_a_iface_name.clone(),
                side_z_iface_name: self.side_z_iface_name.clone(),
                use_onchain_allocation: true,
            },
        );

        client.send_transaction(ix).map(|sig| (sig, pda_pubkey))
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        commands::link::create::CreateLinkCommand, tests::utils::create_test_client,
        DoubleZeroClient,
    };
    use doublezero_serviceability::processors::link::create::LinkCreateArgs;
    use doublezero_serviceability_instruction::link::create_link;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_commands_link_create() {
        let mut client = create_test_client();

        let program_id = client.get_program_id();
        let payer = client.get_payer();
        let contributor_pk = Pubkey::new_unique();
        let side_a_pk = Pubkey::new_unique();
        let side_z_pk = Pubkey::new_unique();

        // create_test_client seeds globalstate.account_index = 0, so the new link
        // index is 1.
        let expected = create_link(
            &program_id,
            &payer,
            &contributor_pk,
            &side_a_pk,
            &side_z_pk,
            1,
            LinkCreateArgs {
                code: "test".to_string(),
                link_type: doublezero_serviceability::state::link::LinkLinkType::DZX,
                desired_status: None,
                bandwidth: 10_000_000_000,
                mtu: 9000,
                delay_ns: 1_000_000,
                jitter_ns: 100_000,
                side_a_iface_name: "Ethernet0".to_string(),
                side_z_iface_name: Some("Ethernet1".to_string()),
                use_onchain_allocation: true,
            },
        );
        client
            .expect_send_transaction()
            .with(predicate::eq(expected))
            .returning(|_| Ok(Signature::new_unique()));

        let res = CreateLinkCommand {
            code: "test".to_string(),
            contributor_pk,
            desired_status: None,
            side_a_pk,
            side_z_pk,
            link_type: doublezero_serviceability::state::link::LinkLinkType::DZX,
            bandwidth: 10_000_000_000,
            mtu: 9000,
            delay_ns: 1_000_000,
            jitter_ns: 100_000,
            side_a_iface_name: "Ethernet0".to_string(),
            side_z_iface_name: Some("Ethernet1".to_string()),
        }
        .execute(&client);

        assert!(res.is_ok());
    }
}
