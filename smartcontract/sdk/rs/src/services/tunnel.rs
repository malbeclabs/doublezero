use eyre::eyre;
use mockall::automock;
use std::collections::HashMap;

use crate::{doublezeroclient::DoubleZeroClient, DZClient};
use double_zero_sla_program::{
    instructions::DoubleZeroInstruction,
    pda::get_tunnel_pda,
    processors::tunnel::{
        activate::TunnelActivateArgs, create::TunnelCreateArgs, deactivate::TunnelDeactivateArgs,
        delete::TunnelDeleteArgs, reactivate::TunnelReactivateArgs, reject::TunnelRejectArgs,
        suspend::TunnelSuspendArgs, update::TunnelUpdateArgs,
    },
    state::{
        accountdata::AccountData,
        accounttype::AccountType,
        tunnel::{Tunnel, TunnelTunnelType},
    },
    types::NetworkV4,
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

#[automock]
pub trait TunnelService {
    fn get_tunnels(&self) -> eyre::Result<HashMap<Pubkey, Tunnel>>;
    fn get_tunnel(&self, pubkey: &Pubkey) -> eyre::Result<Tunnel>;
    #[allow(clippy::too_many_arguments)]
    fn create_tunnel(
        &self,
        code: &str,
        side_a_pk: Pubkey,
        side_z_pk: Pubkey,
        tunnel_type: TunnelTunnelType,
        bandwidth: u64,
        mtu: u32,
        delay_ns: u64,
        jitter_ns: u64,
    ) -> eyre::Result<(Signature, Pubkey)>;
    #[allow(clippy::too_many_arguments)]
    fn update_tunnel(
        &self,
        index: u128,
        code: Option<String>,
        tunnel_type: Option<TunnelTunnelType>,
        bandwidth: Option<u64>,
        mtu: Option<u32>,
        delay_ns: Option<u64>,
        jitter_ns: Option<u64>,
    ) -> eyre::Result<Signature>;
    fn activate_tunnel(
        &self,
        index: u128,
        tunnel_id: u16,
        tunnel_net: NetworkV4,
    ) -> eyre::Result<Signature>;
    fn reject_tunnel(&self, index: u128, error: String) -> eyre::Result<Signature>;
    fn suspend_tunnel(&self, index: u128) -> eyre::Result<Signature>;
    fn reactivate_tunnel(&self, index: u128) -> eyre::Result<Signature>;
    fn delete_tunnel(&self, index: u128) -> eyre::Result<Signature>;
    fn deactivate_tunnel(&self, index: u128, owner: Pubkey) -> eyre::Result<Signature>;
}

pub trait TunnelFinder {
    #![allow(dead_code)]
    fn find_tunnel<P>(&self, predicate: P) -> eyre::Result<(Pubkey, Tunnel)>
    where
        P: Fn(&Tunnel) -> bool + Send + Sync;
}

impl TunnelService for DZClient {
    fn get_tunnels(&self) -> eyre::Result<HashMap<Pubkey, Tunnel>> {
        Ok(self
            .gets(AccountType::Tunnel)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::Tunnel(tunnel) => (k, tunnel),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }

    fn get_tunnel(&self, pubkey: &Pubkey) -> eyre::Result<Tunnel> {
        let account = self.get(*pubkey)?;

        match account {
            AccountData::Tunnel(tunnel) => Ok(tunnel),
            _ => Err(eyre!("Invalid Account Type")),
        }
    }

    fn create_tunnel(
        &self,
        code: &str,
        side_a_pk: Pubkey,
        side_z_pk: Pubkey,
        tunnel_type: TunnelTunnelType,
        bandwidth: u64,
        mtu: u32,
        delay_ns: u64,
        jitter_ns: u64,
    ) -> eyre::Result<(Signature, Pubkey)> {
        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.device_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("Contributor not allowlisted"));
                }

                let (pda_pubkey, _) =
                    get_tunnel_pda(&self.get_program_id(), globalstate.account_index + 1);

                self.execute_transaction(
                    DoubleZeroInstruction::CreateTunnel(TunnelCreateArgs {
                        index: globalstate.account_index + 1,
                        code: code.to_string(),
                        side_a_pk,
                        side_z_pk,
                        tunnel_type,
                        bandwidth,
                        mtu,
                        delay_ns,
                        jitter_ns,
                    }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(side_a_pk, false),
                        AccountMeta::new(side_z_pk, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
                .map(|signature| (signature, pda_pubkey))
            }
            Err(e) => Err(e),
        }
    }

    fn activate_tunnel(
        &self,
        index: u128,
        tunnel_id: u16,
        tunnel_net: NetworkV4,
    ) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_tunnel_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::ActivateTunnel(TunnelActivateArgs {
                        index,
                        tunnel_id,
                        tunnel_net,
                    }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }

    fn reject_tunnel(&self, index: u128, error: String) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_tunnel_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::RejectTunnel(TunnelRejectArgs { index, error }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }

    fn update_tunnel(
        &self,
        index: u128,
        code: Option<String>,
        tunnel_type: Option<TunnelTunnelType>,
        bandwidth: Option<u64>,
        mtu: Option<u32>,
        delay_ns: Option<u64>,
        jitter_ns: Option<u64>,
    ) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_tunnel_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::UpdateTunnel(TunnelUpdateArgs {
                index,
                code,
                tunnel_type,
                bandwidth,
                mtu,
                delay_ns,
                jitter_ns,
            }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn suspend_tunnel(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_tunnel_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::SuspendTunnel(TunnelSuspendArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn reactivate_tunnel(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_tunnel_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::ReactivateTunnel(TunnelReactivateArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn delete_tunnel(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_tunnel_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::DeleteTunnel(TunnelDeleteArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn deactivate_tunnel(&self, index: u128, owner: Pubkey) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_tunnel_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::DeactivateTunnel(TunnelDeactivateArgs { index }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(owner, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }
}

impl TunnelFinder for DZClient {
    fn find_tunnel<P>(&self, predicate: P) -> eyre::Result<(Pubkey, Tunnel)>
    where
        P: Fn(&Tunnel) -> bool + Send + Sync,
    {
        let tunnels = self.get_tunnels()?;

        match tunnels.into_iter().find(|(_, tunnel)| predicate(tunnel)) {
            Some((pubkey, tunnel)) => Ok((pubkey, tunnel)),
            None => Err(eyre!("Tunnel not found")),
        }
    }
}
