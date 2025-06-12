use crate::{
    doublezerocommand::CliCommand,
    helpers::parse_pubkey,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::multicastgroup::{
    get::GetMulticastGroupCommand, subscribe::SubscribeMulticastGroupCommand,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct SubscribeUserCliCommand {
    /// User Pubkey to subscribe
    #[arg(long)]
    pub user: String,
    /// Multicast group Pubkey or code to subscribe to
    #[arg(long)]
    pub group: String,
    /// Subscribe as a publisher
    #[arg(long)]
    pub publisher: bool,
    /// Subscribe as a subscriber
    #[arg(long)]
    pub subscriber: bool,
}

impl SubscribeUserCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let user_pk =
            parse_pubkey(&self.user).ok_or_else(|| eyre::eyre!("Invalid user: {}", self.user))?;

        let group_pk = match parse_pubkey(&self.group) {
            Some(pk) => Some(pk),
            None => {
                let (pubkey, _) = client
                    .get_multicastgroup(GetMulticastGroupCommand {
                        pubkey_or_code: self.group.to_string(),
                    })
                    .map_err(|_| eyre::eyre!("MulticastGroup not found ({})", self.group))?;
                Some(pubkey)
            }
        }
        .ok_or_else(|| eyre::eyre!("Invalid group: {}", self.group))?;

        let signature = client.subscribe_multicastgroup(SubscribeMulticastGroupCommand {
            user_pk,
            group_pk,
            publisher: self.publisher,
            subscriber: self.subscriber,
        })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use crate::{
        doublezerocommand::CliCommand,
        requirements::{CHECK_BALANCE, CHECK_ID_JSON},
        tests::utils::create_test_client,
        user::subscribe::SubscribeUserCliCommand,
    };
    use doublezero_sdk::{
        commands::multicastgroup::{
            get::GetMulticastGroupCommand, subscribe::SubscribeMulticastGroupCommand,
        },
        AccountType, MulticastGroup, MulticastGroupStatus,
    };
    use doublezero_serviceability::pda::get_user_pda;
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_user_create_subscribe() {
        let mut client = create_test_client();

        let (user_pubkey, _bump_seed) = get_user_pda(&client.get_program_id(), 1);
        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);
        let mgroup_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let mgroup = MulticastGroup {
            account_type: AccountType::MulticastGroup,
            index: 1,
            bump_seed: 255,
            tenant_pk: Pubkey::new_unique(),
            multicast_ip: [239, 1, 1, 1],
            max_bandwidth: 1000,
            status: MulticastGroupStatus::Activated,
            code: "test".to_string(),
            pub_allowlist: vec![],
            sub_allowlist: vec![],
            publishers: vec![],
            subscribers: vec![],
            owner: mgroup_pubkey,
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
            .returning(move |_| Ok((mgroup_pubkey, mgroup.clone())));
        client
            .expect_subscribe_multicastgroup()
            .with(predicate::eq(SubscribeMulticastGroupCommand {
                user_pk: user_pubkey,
                group_pk: mgroup_pubkey,
                publisher: false,
                subscriber: true,
            }))
            .times(1)
            .returning(move |_| Ok(signature));

        /*****************************************************************************************************/
        let mut output = Vec::new();
        let res = SubscribeUserCliCommand {
            user: user_pubkey.to_string(),
            group: mgroup_pubkey.to_string(),
            publisher: false,
            subscriber: true,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
