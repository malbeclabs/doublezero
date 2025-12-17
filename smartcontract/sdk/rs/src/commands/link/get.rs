use crate::{utils::parse_pubkey, DoubleZeroClient};
use doublezero_serviceability::state::{
    accountdata::AccountData, accounttype::AccountType, link::Link,
};
use solana_sdk::pubkey::Pubkey;

#[derive(Debug, PartialEq, Clone)]
pub struct GetLinkCommand {
    pub pubkey_or_code: String,
}

impl GetLinkCommand {
    pub fn execute(&self, client: &dyn DoubleZeroClient) -> eyre::Result<(Pubkey, Link)> {
        match parse_pubkey(&self.pubkey_or_code) {
            Some(pk) => match client.get(pk)? {
                AccountData::Link(tunnel) => Ok((pk, tunnel)),
                _ => Err(eyre::eyre!("Invalid Account Type")),
            },
            None => client
                .gets(AccountType::Link)?
                .into_iter()
                .find(|(_, v)| match v {
                    AccountData::Link(tunnel) => {
                        tunnel.code.eq_ignore_ascii_case(&self.pubkey_or_code)
                    }
                    _ => false,
                })
                .map(|(pk, v)| match v {
                    AccountData::Link(tunnel) => Ok((pk, tunnel)),
                    _ => Err(eyre::eyre!("Invalid Account Type")),
                })
                .unwrap_or_else(|| {
                    Err(eyre::eyre!(
                        "Link with code {} not found",
                        self.pubkey_or_code
                    ))
                }),
        }
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::{commands::link::get::GetLinkCommand, tests::utils::create_test_client};
    use doublezero_serviceability::state::{
        accountdata::AccountData, accounttype::AccountType, link::Link,
    };
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_commands_link_get_command() {
        let mut client = create_test_client();

        let link_pubkey = Pubkey::new_unique();
        let link = Link {
            account_type: AccountType::Link,
            index: 1,
            bump_seed: 2,
            code: "link_code".to_string(),
            owner: Pubkey::new_unique(),
            ..Default::default()
        };

        let link2 = link.clone();
        client
            .expect_get()
            .with(predicate::eq(link_pubkey))
            .returning(move |_| Ok(AccountData::Link(link2.clone())));

        let link2 = link.clone();
        client
            .expect_gets()
            .with(predicate::eq(AccountType::Link))
            .returning(move |_| {
                Ok(HashMap::from([(
                    link_pubkey,
                    AccountData::Link(link2.clone()),
                )]))
            });

        // Search by pubkey
        let res = GetLinkCommand {
            pubkey_or_code: link_pubkey.to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "link_code".to_string());
        assert_eq!(res.1.owner, link.owner);

        // Search by code
        let res = GetLinkCommand {
            pubkey_or_code: "link_code".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "link_code".to_string());
        assert_eq!(res.1.owner, link.owner);

        // Search by code UPPERCASE
        let res = GetLinkCommand {
            pubkey_or_code: "LINK_CODE".to_string(),
        }
        .execute(&client);

        assert!(res.is_ok());
        let res = res.unwrap();
        assert_eq!(res.1.code, "link_code".to_string());
        assert_eq!(res.1.owner, link.owner);

        // Invalid search
        let res = GetLinkCommand {
            pubkey_or_code: "ssssssssssss".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());

        // Search by invalid code
        let res = GetLinkCommand {
            pubkey_or_code: "s(%".to_string(),
        }
        .execute(&client);

        assert!(res.is_err());
    }
}
