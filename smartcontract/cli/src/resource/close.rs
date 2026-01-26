use super::ResourceType;
use crate::{
    doublezerocommand::CliCommand,
    requirements::{CHECK_BALANCE, CHECK_ID_JSON},
};
use clap::Args;
use doublezero_sdk::commands::resource::{
    closeaccount::CloseResourceCommand, get::GetResourceCommand,
};
use std::io::Write;

#[derive(Args, Debug)]
pub struct CloseResourceCliCommand {
    // Type of resource extension to allocate
    #[arg(long)]
    pub resource_type: ResourceType,
    // Associated public key (only for DzPrefixBlock)
    #[arg(long)]
    pub associated_pubkey: Option<String>,
    // Index (only for DzPrefixBlock)
    #[arg(long)]
    pub index: Option<usize>,
}

impl CloseResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        // Check requirements
        client.check_requirements(CHECK_ID_JSON | CHECK_BALANCE)?;

        let resource_type = super::resource_type_from(
            self.resource_type,
            self.associated_pubkey.as_ref().and_then(|s| s.parse().ok()),
            self.index,
        );

        let (_, _) = client
            .get_resource(GetResourceCommand { resource_type })
            .map_err(|_| eyre::eyre!("Resource not found"))?;

        let signature = client.close_resource(CloseResourceCommand { resource_type })?;
        writeln!(out, "Signature: {signature}",)?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::tests::utils::create_test_client;
    use doublezero_sdk::{
        commands::resource::{closeaccount::CloseResourceCommand, get::GetResourceCommand},
        get_resource_extension_pda, AccountType, ResourceExtensionOwned,
    };
    use mockall::predicate;
    use solana_sdk::{pubkey::Pubkey, signature::Signature};

    #[test]
    fn test_cli_resource_close() {
        let mut client = create_test_client();

        let (pda_pubkey, _, _) = get_resource_extension_pda(
            &client.get_program_id(),
            doublezero_sdk::ResourceType::DeviceTunnelBlock,
        );

        let owner = Pubkey::new_unique();

        let signature = Signature::from([
            120, 138, 162, 185, 59, 209, 241, 157, 71, 157, 74, 131, 4, 87, 54, 28, 38, 180, 222,
            82, 64, 62, 61, 62, 22, 46, 17, 203, 187, 136, 62, 43, 11, 38, 235, 17, 239, 82, 240,
            139, 130, 217, 227, 214, 9, 242, 141, 223, 94, 29, 184, 110, 62, 32, 87, 137, 63, 139,
            100, 221, 20, 137, 4, 5,
        ]);

        let resource = ResourceExtensionOwned {
            account_type: AccountType::ResourceExtension,
            owner,
            bump_seed: 0,
            associated_with: Pubkey::default(),
            allocator: doublezero_serviceability::state::resource_extension::Allocator::Ip(
                doublezero_serviceability::ip_allocator::IpAllocator::new(
                    "10.0.0.0/24".parse().unwrap(),
                ),
            ),
            storage: vec![0x0; 8],
        };

        let resource_type = doublezero_sdk::ResourceType::DeviceTunnelBlock;

        client
            .expect_check_requirements()
            .with(predicate::eq(CHECK_ID_JSON | CHECK_BALANCE))
            .returning(|_| Ok(()));
        client
            .expect_get_resource()
            .with(predicate::eq(GetResourceCommand { resource_type }))
            .returning(move |_| Ok((pda_pubkey, resource.clone())));

        client
            .expect_close_resource()
            .with(predicate::eq(CloseResourceCommand { resource_type }))
            .returning(move |_| Ok(signature));

        let mut output = Vec::new();
        let res = CloseResourceCliCommand {
            resource_type: ResourceType::DeviceTunnelBlock,
            associated_pubkey: None,
            index: None,
        }
        .execute(&client, &mut output);
        assert!(res.is_ok());
        let output_str = String::from_utf8(output).unwrap();
        assert_eq!(
            output_str,"Signature: 3QnHBSdd4doEF6FgpLCejqEw42UQjfvNhQJwoYDSpoBszpCCqVft4cGoneDCnZ6Ez3ujzavzUu85u6F79WtLhcsv\n"
        );
    }
}
