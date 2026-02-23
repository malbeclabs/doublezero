use doublezero_geolocation::state::{
    geo_probe::GeoProbe, program_config::GeolocationProgramConfig,
};
use doublezero_sdk::geolocation::{
    client::GeolocationClient,
    geo_probe::{
        add_parent_device::AddParentDeviceCommand, create::CreateGeoProbeCommand,
        delete::DeleteGeoProbeCommand, get::GetGeoProbeCommand, list::ListGeoProbeCommand,
        remove_parent_device::RemoveParentDeviceCommand, update::UpdateGeoProbeCommand,
    },
    programconfig::{get::GetProgramConfigCommand, init::InitProgramConfigCommand},
};
use mockall::automock;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::collections::HashMap;

#[automock]
pub trait GeoCliCommand {
    fn get_geo_program_id(&self) -> Pubkey;
    fn get_serviceability_globalstate_pk(&self) -> Pubkey;
    fn get_payer(&self) -> Pubkey;

    fn create_geo_probe(&self, cmd: CreateGeoProbeCommand) -> eyre::Result<(Signature, Pubkey)>;
    fn update_geo_probe(&self, cmd: UpdateGeoProbeCommand) -> eyre::Result<Signature>;
    fn delete_geo_probe(&self, cmd: DeleteGeoProbeCommand) -> eyre::Result<Signature>;
    fn get_geo_probe(&self, cmd: GetGeoProbeCommand) -> eyre::Result<(Pubkey, GeoProbe)>;
    fn list_geo_probes(&self, cmd: ListGeoProbeCommand) -> eyre::Result<HashMap<Pubkey, GeoProbe>>;
    fn add_parent_device(&self, cmd: AddParentDeviceCommand) -> eyre::Result<Signature>;
    fn remove_parent_device(&self, cmd: RemoveParentDeviceCommand) -> eyre::Result<Signature>;
    fn init_program_config(
        &self,
        cmd: InitProgramConfigCommand,
    ) -> eyre::Result<(Signature, Pubkey)>;
    fn get_program_config(
        &self,
        cmd: GetProgramConfigCommand,
    ) -> eyre::Result<(Pubkey, GeolocationProgramConfig)>;
}

pub struct GeoCliCommandImpl<'a> {
    client: &'a doublezero_sdk::geolocation::client::GeoClient,
    serviceability_globalstate_pk: Pubkey,
}

impl<'a> GeoCliCommandImpl<'a> {
    pub fn new(
        client: &'a doublezero_sdk::geolocation::client::GeoClient,
        serviceability_globalstate_pk: Pubkey,
    ) -> Self {
        Self {
            client,
            serviceability_globalstate_pk,
        }
    }
}

impl GeoCliCommand for GeoCliCommandImpl<'_> {
    fn get_geo_program_id(&self) -> Pubkey {
        self.client.get_program_id()
    }

    fn get_serviceability_globalstate_pk(&self) -> Pubkey {
        self.serviceability_globalstate_pk
    }

    fn get_payer(&self) -> Pubkey {
        self.client.get_payer()
    }

    fn create_geo_probe(&self, cmd: CreateGeoProbeCommand) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }

    fn update_geo_probe(&self, cmd: UpdateGeoProbeCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn delete_geo_probe(&self, cmd: DeleteGeoProbeCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn get_geo_probe(&self, cmd: GetGeoProbeCommand) -> eyre::Result<(Pubkey, GeoProbe)> {
        cmd.execute(self.client)
    }

    fn list_geo_probes(&self, cmd: ListGeoProbeCommand) -> eyre::Result<HashMap<Pubkey, GeoProbe>> {
        cmd.execute(self.client)
    }

    fn add_parent_device(&self, cmd: AddParentDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn remove_parent_device(&self, cmd: RemoveParentDeviceCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn init_program_config(
        &self,
        cmd: InitProgramConfigCommand,
    ) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }

    fn get_program_config(
        &self,
        cmd: GetProgramConfigCommand,
    ) -> eyre::Result<(Pubkey, GeolocationProgramConfig)> {
        cmd.execute(self.client)
    }
}
