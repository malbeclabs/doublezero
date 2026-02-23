use crate::{commands::globalstate::get::GetGlobalStateCommand, DoubleZeroClient};
use doublezero_program_common::validate_account_code;
use doublezero_serviceability::{
    instructions::DoubleZeroInstruction,
    pda::get_link_pda,
    processors::link::create::LinkCreateArgs,
    state::link::{LinkDesiredStatus, LinkLinkType},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

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

        let (globalstate_pubkey, globalstate) = GetGlobalStateCommand
            .execute(client)
            .map_err(|_err| eyre::eyre!("Globalstate not initialized"))?;

        let (pda_pubkey, _) = get_link_pda(&client.get_program_id(), globalstate.account_index + 1);
        client
            .execute_transaction(
                DoubleZeroInstruction::CreateLink(LinkCreateArgs {
                    code,
                    link_type: self.link_type,
                    desired_status: self.desired_status,
                    bandwidth: self.bandwidth,
                    mtu: self.mtu,
                    delay_ns: self.delay_ns,
                    jitter_ns: self.jitter_ns,
                    side_a_iface_name: self.side_a_iface_name.clone(),
                    side_z_iface_name: self.side_z_iface_name.clone(),
                }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(self.contributor_pk, false),
                    AccountMeta::new(self.side_a_pk, false),
                    AccountMeta::new(self.side_z_pk, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            )
            .map(|sig| (sig, pda_pubkey))
    }
}
