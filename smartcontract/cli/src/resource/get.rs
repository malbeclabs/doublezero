use super::ResourceType;
use crate::doublezerocommand::CliCommand;
use clap::Args;
use doublezero_sdk::commands::resource::get::GetResourceCommand;
use std::io::Write;
use tabled::{Table, Tabled};

#[derive(Args, Debug)]
pub struct GetResourceCliCommand {
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

impl From<GetResourceCliCommand> for GetResourceCommand {
    fn from(cmd: GetResourceCliCommand) -> Self {
        let resource_type = super::resource_type_from(
            cmd.resource_type,
            cmd.associated_pubkey.as_ref().and_then(|s| s.parse().ok()),
            cmd.index,
        );

        GetResourceCommand { resource_type }
    }
}

#[derive(Tabled)]
pub struct ResourceDisplay {
    #[tabled(rename = "Allocated Resources")]
    pub resource: String,
}

impl GetResourceCliCommand {
    pub fn execute<C: CliCommand, W: Write>(self, client: &C, out: &mut W) -> eyre::Result<()> {
        let (_, resource_extension) = client.get_resource(self.into())?;

        let resource_displays = resource_extension
            .iter_allocated()
            .into_iter()
            .map(|res| ResourceDisplay {
                resource: res.to_string(),
            })
            .collect::<Vec<_>>();
        let table = Table::new(resource_displays).to_string();
        writeln!(out, "{table}")?;

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::doublezerocommand::MockCliCommand;
    use doublezero_sdk::{AccountType, ResourceType as SdkResourceType};
    use doublezero_serviceability::{
        id_allocator::IdAllocator,
        state::resource_extension::{Allocator, ResourceExtensionOwned},
    };
    use solana_program::pubkey::Pubkey;
    use std::io::Cursor;

    #[test]
    fn test_from_cli_to_command() {
        let pk = Pubkey::new_unique();
        let cli_cmd = GetResourceCliCommand {
            resource_type: ResourceType::DzPrefixBlock,
            associated_pubkey: Some(pk.to_string()),
            index: Some(1),
        };
        let cmd: GetResourceCommand = cli_cmd.into();
        assert_eq!(cmd.resource_type, SdkResourceType::DzPrefixBlock(pk, 1));
    }

    #[test]
    fn test_execute_prints_table() {
        let cli_cmd = GetResourceCliCommand {
            resource_type: ResourceType::LinkIds,
            associated_pubkey: None,
            index: None,
        };
        let mut mock_client = MockCliCommand::new();
        let resource_ext = ResourceExtensionOwned {
            account_type: AccountType::ResourceExtension,
            owner: Pubkey::default(),
            bump_seed: 0,
            associated_with: Pubkey::default(),
            allocator: Allocator::Id(IdAllocator::new((0, 16)).unwrap()),
            storage: vec![0xff; 1],
        };
        mock_client
            .expect_get_resource()
            .withf(|cmd: &GetResourceCommand| cmd.resource_type == SdkResourceType::LinkIds)
            .returning(move |_| Ok((Pubkey::default(), resource_ext.clone())));
        let mut output = Cursor::new(Vec::new());
        let result = cli_cmd.execute(&mock_client, &mut output);
        assert!(result.is_ok());
        let output_str = String::from_utf8(output.into_inner()).unwrap();
        assert_eq!(
            output_str,
            "\
+---------------------+
| Allocated Resources |
+---------------------+
| 0                   |
+---------------------+
| 1                   |
+---------------------+
| 2                   |
+---------------------+
| 3                   |
+---------------------+
| 4                   |
+---------------------+
| 5                   |
+---------------------+
| 6                   |
+---------------------+
| 7                   |
+---------------------+
"
        );
    }
}
