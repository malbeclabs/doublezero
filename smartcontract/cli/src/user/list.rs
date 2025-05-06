use clap::Args;
use doublezero_sdk::commands::device::list::ListDeviceCommand;
use doublezero_sdk::commands::user::list::ListUserCommand;
use doublezero_sdk::*;
use prettytable::{format, row, Cell, Row, Table};
use solana_sdk::pubkey::Pubkey;
use std::io::Write;

#[derive(Args, Debug)]
pub struct ListUserArgs {
    #[arg(long)]
    pub code: Option<String>,
}

impl ListUserArgs {
    pub fn execute<W: Write>(self, client: &dyn DoubleZeroClient, out: &mut W) -> eyre::Result<()> {
        let mut table = Table::new();
        table.add_row(row![
            "account",
            "user_type",
            "device",
            "cyoa_type",
            "client_ip",
            "tunnel_id",
            "tunnel_net",
            "dz_ip",
            "status",
            "owner"
        ]);

        let devices = ListDeviceCommand {}.execute(client)?;

        let users = ListUserCommand {}.execute(client)?;

        let mut users: Vec<(Pubkey, User)> = users.into_iter().collect();
        users.sort_by(|(_, a), (_, b)| {
            a.device_pk
                .cmp(&b.device_pk)
                .then(a.tunnel_id.cmp(&b.tunnel_id))
        });

        for (pubkey, data) in users {
            let device_name = match &devices.get(&data.device_pk) {
                Some(device) => &device.code,
                None => &data.device_pk.to_string(),
            };

            table.add_row(Row::new(vec![
                Cell::new(&pubkey.to_string()),
                Cell::new(&data.user_type.to_string()),
                Cell::new(device_name),
                Cell::new(&data.cyoa_type.to_string()),
                Cell::new(&ipv4_to_string(&data.client_ip)),
                Cell::new(&data.tunnel_id.to_string()),
                Cell::new(&networkv4_to_string(&data.tunnel_net)),
                Cell::new(&ipv4_to_string(&data.dz_ip)),
                Cell::new(&data.status.to_string()),
                Cell::new(&data.owner.to_string()),
            ]));
        }

        table.set_format(*format::consts::FORMAT_NO_BORDER_LINE_SEPARATOR);
        let _ = table.print(out);

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use std::collections::HashMap;

    use crate::tests::tests::create_test_client;
    use crate::user::list::ListUserArgs;
    use crate::user::list::UserCYOA::GREOverDIA;
    use crate::user::list::UserStatus::Activated;
    use crate::user::list::UserType::IBRL;
    use doublezero_sdk::{AccountType, Device, DeviceStatus, DeviceType, User};

    use doublezero_sla_program::state::accountdata::AccountData;
    use mockall::predicate;
    use solana_sdk::pubkey::Pubkey;

    #[test]
    fn test_cli_user_list() {
        let mut client = create_test_client();

        let location1_pubkey = Pubkey::new_unique();
        let location2_pubkey = Pubkey::new_unique();
        let exchange1_pubkey = Pubkey::new_unique();
        let exchange2_pubkey = Pubkey::new_unique();

        let device1_pubkey = Pubkey::new_unique();
        let device1 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            code: "device1_code".to_string(),
            location_pk: location1_pubkey,
            exchange_pk: exchange1_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: Pubkey::new_unique(),
        };
        let device2_pubkey = Pubkey::new_unique();
        let device2 = Device {
            account_type: AccountType::Device,
            index: 1,
            bump_seed: 2,
            code: "device2_code".to_string(),
            location_pk: location2_pubkey,
            exchange_pk: exchange2_pubkey,
            device_type: DeviceType::Switch,
            public_ip: [1, 2, 3, 4],
            dz_prefixes: vec![([1, 2, 3, 4], 32)],
            status: DeviceStatus::Activated,
            owner: Pubkey::new_unique(),
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::Device))
            .returning(move |_| {
                let mut devices = HashMap::new();
                devices.insert(device1_pubkey, AccountData::Device(device1.clone()));
                devices.insert(device2_pubkey, AccountData::Device(device2.clone()));
                Ok(devices)
            });

        let user1_pubkey = Pubkey::from_str_const("11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo");
        let user1 = User {
            account_type: AccountType::User,
            index: 1,
            bump_seed: 2,
            owner: user1_pubkey,
            user_type: IBRL,
            tenant_pk: Pubkey::new_unique(),
            device_pk: Pubkey::from_str_const("11111116EPqoQskEM2Pddp8KTL9JdYEBZMGF3aq7V"),
            cyoa_type: GREOverDIA,
            client_ip: [1, 2, 3, 4],
            dz_ip: [2, 3, 4, 5],
            tunnel_id: 500,
            tunnel_net: ([1, 2, 3, 5], 32).into(),
            status: Activated,
        };

        client
            .expect_gets()
            .with(predicate::eq(AccountType::User))
            .returning(move |_| {
                let mut users = HashMap::new();
                users.insert(user1_pubkey, AccountData::User(user1.clone()));
                Ok(users)
            });

        let mut output = Vec::new();
        let res = ListUserArgs { code: None }.execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();

        assert_eq!(output_str, " pubkey                                    | user_type | device                                    | cyoa_type  | client_ip | tunnel_id | tunnel_net | dz_ip   | status    | owner \n 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo | IBRL      | 11111116EPqoQskEM2Pddp8KTL9JdYEBZMGF3aq7V | GREOverDIA | 1.2.3.4   | 500       | 1.2.3.5/32 | 2.3.4.5 | activated | 11111115RidqCHAoz6dzmXxGcfWLNzevYqNpaRAUo \n");
    }
}
