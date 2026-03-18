use doublezero_geolocation::state::{geo_probe::GeoProbe, geolocation_user::GeolocationUser};
use doublezero_sdk::{
    commands::exchange::get::GetExchangeCommand,
    geolocation::{
        geo_probe::{
            add_parent_device::AddParentDeviceCommand, create::CreateGeoProbeCommand,
            delete::DeleteGeoProbeCommand, get::GetGeoProbeCommand, list::ListGeoProbeCommand,
            remove_parent_device::RemoveParentDeviceCommand, update::UpdateGeoProbeCommand,
        },
        geolocation_user::{
            add_target::AddTargetCommand, create::CreateGeolocationUserCommand,
            delete::DeleteGeolocationUserCommand, get::GetGeolocationUserCommand,
            list::ListGeolocationUserCommand, remove_target::RemoveTargetCommand,
            update_payment_status::UpdatePaymentStatusCommand,
        },
        programconfig::init::InitProgramConfigCommand,
    },
    DoubleZeroClient,
};
use mockall::automock;
use solana_sdk::{pubkey::Pubkey, signature::Signature};
use std::collections::HashMap;

#[automock]
pub trait GeoCliCommand {
    fn get_serviceability_globalstate_pk(&self) -> Pubkey;

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

    fn create_geolocation_user(
        &self,
        cmd: CreateGeolocationUserCommand,
    ) -> eyre::Result<(Signature, Pubkey)>;
    fn delete_geolocation_user(&self, cmd: DeleteGeolocationUserCommand)
        -> eyre::Result<Signature>;
    fn get_geolocation_user(
        &self,
        cmd: GetGeolocationUserCommand,
    ) -> eyre::Result<(Pubkey, GeolocationUser)>;
    fn list_geolocation_users(
        &self,
        cmd: ListGeolocationUserCommand,
    ) -> eyre::Result<HashMap<Pubkey, GeolocationUser>>;
    fn add_target(&self, cmd: AddTargetCommand) -> eyre::Result<Signature>;
    fn remove_target(&self, cmd: RemoveTargetCommand) -> eyre::Result<Signature>;
    fn update_payment_status(&self, cmd: UpdatePaymentStatusCommand) -> eyre::Result<Signature>;

    fn resolve_exchange_pk(&self, pubkey_or_code: String) -> eyre::Result<Pubkey>;
}

pub struct GeoCliCommandImpl<'a> {
    client: &'a doublezero_sdk::geolocation::client::GeoClient,
    svc_client: &'a dyn DoubleZeroClient,
    serviceability_globalstate_pk: Pubkey,
}

impl<'a> GeoCliCommandImpl<'a> {
    pub fn new(
        client: &'a doublezero_sdk::geolocation::client::GeoClient,
        svc_client: &'a dyn DoubleZeroClient,
        serviceability_globalstate_pk: Pubkey,
    ) -> Self {
        Self {
            client,
            svc_client,
            serviceability_globalstate_pk,
        }
    }
}

impl GeoCliCommand for GeoCliCommandImpl<'_> {
    fn get_serviceability_globalstate_pk(&self) -> Pubkey {
        self.serviceability_globalstate_pk
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

    fn create_geolocation_user(
        &self,
        cmd: CreateGeolocationUserCommand,
    ) -> eyre::Result<(Signature, Pubkey)> {
        cmd.execute(self.client)
    }

    fn delete_geolocation_user(
        &self,
        cmd: DeleteGeolocationUserCommand,
    ) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn get_geolocation_user(
        &self,
        cmd: GetGeolocationUserCommand,
    ) -> eyre::Result<(Pubkey, GeolocationUser)> {
        cmd.execute(self.client)
    }

    fn list_geolocation_users(
        &self,
        cmd: ListGeolocationUserCommand,
    ) -> eyre::Result<HashMap<Pubkey, GeolocationUser>> {
        cmd.execute(self.client)
    }

    fn add_target(&self, cmd: AddTargetCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn remove_target(&self, cmd: RemoveTargetCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn update_payment_status(&self, cmd: UpdatePaymentStatusCommand) -> eyre::Result<Signature> {
        cmd.execute(self.client)
    }

    fn resolve_exchange_pk(&self, pubkey_or_code: String) -> eyre::Result<Pubkey> {
        let (pk, _) = GetExchangeCommand { pubkey_or_code }.execute(self.svc_client)?;
        Ok(pk)
    }
}
