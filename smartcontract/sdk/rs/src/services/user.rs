use eyre::eyre;
use std::collections::HashMap;

use crate::{doublezeroclient::DoubleZeroClient, DZClient};
use double_zero_sla_program::{
    instructions::DoubleZeroInstruction,
    pda::{get_globalconfig_pda, get_user_pda},
    processors::user::{
        activate::UserActivateArgs, ban::UserBanArgs, create::UserCreateArgs,
        deactivate::UserDeactivateArgs, delete::UserDeleteArgs, reactivate::UserReactivateArgs,
        reject::UserRejectArgs, requestban::UserRequestBanArgs, suspend::UserSuspendArgs,
        update::UserUpdateArgs,
    },
    state::{
        accountdata::AccountData,
        accounttype::AccountType,
        user::{User, UserCYOA, UserType},
    },
    types::{IpV4, NetworkV4},
};
use solana_sdk::{instruction::AccountMeta, pubkey::Pubkey, signature::Signature};

pub trait UserService {
    fn get_users(&self) -> eyre::Result<HashMap<Pubkey, User>>;
    fn get_user(&self, pubkey: &Pubkey) -> eyre::Result<User>;
    fn find_user<P>(&self, predicate: P) -> eyre::Result<(Pubkey, User)>
    where
        P: Fn(&User) -> bool + Send + Sync;
    fn create_user(
        &self,
        user_type: UserType,
        device_pk: Pubkey,
        cyoa_type: UserCYOA,
        client_ip: IpV4,
    ) -> eyre::Result<(Signature, Pubkey)>;
    #[allow(clippy::too_many_arguments)]
    fn update_user(
        &self,
        index: u128,
        user_type: Option<UserType>,
        cyoa_type: Option<UserCYOA>,
        client_ip: Option<IpV4>,
        dz_ip: Option<IpV4>,
        tunnel_id: Option<u16>,
        tunnel_net: Option<NetworkV4>,
    ) -> eyre::Result<Signature>;
    fn activate_user(
        &self,
        index: u128,
        tunnel_id: u16,
        tunnel_net: NetworkV4,
        dz_ip: IpV4,
    ) -> eyre::Result<Signature>;
    fn reject_user(&self, index: u128, error: String) -> eyre::Result<Signature>;
    fn suspend_user(&self, index: u128) -> eyre::Result<Signature>;
    fn reactivate_user(&self, index: u128) -> eyre::Result<Signature>;
    fn delete_user(&self, index: u128) -> eyre::Result<Signature>;
    fn deactivate_user(&self, index: u128, owner: Pubkey) -> eyre::Result<Signature>;
    fn request_ban_user(&self, index: u128) -> eyre::Result<Signature>;
    fn ban_user(&self, index: u128) -> eyre::Result<Signature>;
}

impl UserService for DZClient {
    fn get_users(&self) -> eyre::Result<HashMap<Pubkey, User>> {
        Ok(self
            .gets(AccountType::User)?
            .into_iter()
            .map(|(k, v)| match v {
                AccountData::User(user) => (k, user),
                _ => panic!("Invalid Account Type"),
            })
            .collect())
    }

    fn get_user(&self, pubkey: &Pubkey) -> eyre::Result<User> {
        let account = self.get(*pubkey)?;

        match account {
            AccountData::User(user) => Ok(user),
            _ => Err(eyre!("Invalid Account Type")),
        }
    }

    fn find_user<P>(&self, predicate: P) -> eyre::Result<(Pubkey, User)>
    where
        P: Fn(&User) -> bool + Send + Sync,
    {
        let users = self.get_users()?;

        match users.into_iter().find(|(_, user)| predicate(user)) {
            Some((pubkey, user)) => Ok((pubkey, user)),
            None => Err(eyre!("User not found")),
        }
    }

    fn create_user(
        &self,
        user_type: UserType,
        device_pk: Pubkey,
        cyoa_type: UserCYOA,
        client_ip: IpV4,
    ) -> eyre::Result<(Signature, Pubkey)> {
        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.user_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                let (pda_pubkey, _) =
                    get_user_pda(&self.get_program_id(), globalstate.account_index + 1);

                self.execute_transaction(
                    DoubleZeroInstruction::CreateUser(UserCreateArgs {
                        index: globalstate.account_index + 1,
                        user_type,
                        device_pk,
                        cyoa_type,
                        client_ip,
                    }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(device_pk, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
                .map(|signature| (signature, pda_pubkey))
            }
            Err(e) => Err(e),
        }
    }

    fn update_user(
        &self,
        index: u128,
        user_type: Option<UserType>,
        cyoa_type: Option<UserCYOA>,
        client_ip: Option<IpV4>,
        dz_ip: Option<IpV4>,
        tunnel_id: Option<u16>,
        tunnel_net: Option<NetworkV4>,
    ) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_user_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::UpdateUser(UserUpdateArgs {
                        index,
                        user_type,
                        cyoa_type,
                        client_ip,
                        dz_ip,
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

    fn activate_user(
        &self,
        index: u128,
        tunnel_id: u16,
        tunnel_net: NetworkV4,
        dz_ip: IpV4,
    ) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_user_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::ActivateUser(UserActivateArgs {
                        index,
                        tunnel_id,
                        tunnel_net,
                        dz_ip,
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

    fn reject_user(&self, index: u128, error: String) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_user_pda(&self.get_program_id(), index);
        let (pda_config, _) = get_globalconfig_pda(&self.get_program_id());

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }
                self.execute_transaction(
                    DoubleZeroInstruction::RejectUser(UserRejectArgs { index, error }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(pda_config, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }

    fn suspend_user(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_user_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::SuspendUser(UserSuspendArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn reactivate_user(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_user_pda(&self.get_program_id(), index);

        self.execute_transaction(
            DoubleZeroInstruction::ReactivateUser(UserReactivateArgs { index }),
            vec![AccountMeta::new(pda_pubkey, false)],
        )
    }

    fn delete_user(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_user_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, _globalstate)) => self.execute_transaction(
                DoubleZeroInstruction::DeleteUser(UserDeleteArgs { index }),
                vec![
                    AccountMeta::new(pda_pubkey, false),
                    AccountMeta::new(globalstate_pubkey, false),
                ],
            ),
            Err(e) => Err(e),
        }
    }

    fn deactivate_user(&self, index: u128, owner: Pubkey) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_user_pda(&self.get_program_id(), index);

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::DeactivateUser(UserDeactivateArgs { index }),
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

    fn request_ban_user(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_user_pda(&self.get_program_id(), index);
        let (pda_config, _) = get_globalconfig_pda(&self.get_program_id());

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }

                self.execute_transaction(
                    DoubleZeroInstruction::RequestBanUser(UserRequestBanArgs { index }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(pda_config, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }

    fn ban_user(&self, index: u128) -> eyre::Result<Signature> {
        let (pda_pubkey, _) = get_user_pda(&self.get_program_id(), index);
        let (pda_config, _) = get_globalconfig_pda(&self.get_program_id());

        match self.get_globalstate() {
            Ok((globalstate_pubkey, globalstate)) => {
                if !globalstate.foundation_allowlist.contains(&self.get_payer()) {
                    return Err(eyre!("User not allowlisted"));
                }
                self.execute_transaction(
                    DoubleZeroInstruction::BanUser(UserBanArgs { index }),
                    vec![
                        AccountMeta::new(pda_pubkey, false),
                        AccountMeta::new(pda_config, false),
                        AccountMeta::new(globalstate_pubkey, false),
                    ],
                )
            }
            Err(e) => Err(e),
        }
    }
}
