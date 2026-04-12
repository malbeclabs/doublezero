use std::collections::HashMap;

use solana_sdk::pubkey::Pubkey;

use crate::dz_ledger_reader::DzDeviceInfo;

/// Returns true if the device can accept a new multicast publisher.
pub fn device_has_multicast_publisher_capacity(device: &DzDeviceInfo) -> bool {
    if device.max_users == 0 {
        return false; // locked
    }
    if device.users_count + device.reserved_seats >= device.max_users {
        return false; // total user capacity exceeded
    }
    if device.max_multicast_publishers > 0
        && device.multicast_publishers_count >= device.max_multicast_publishers
    {
        return false; // per-type publisher limit exceeded
    }
    true
}

/// Great-circle distance between two lat/lng points in kilometres (Haversine formula).
pub fn haversine_km(lat1: f64, lng1: f64, lat2: f64, lng2: f64) -> f64 {
    const R: f64 = 6371.0;
    let dlat = (lat2 - lat1).to_radians();
    let dlng = (lng2 - lng1).to_radians();
    let a = (dlat / 2.0).sin().powi(2)
        + lat1.to_radians().cos() * lat2.to_radians().cos() * (dlng / 2.0).sin().powi(2);
    2.0 * R * a.sqrt().asin()
}

/// Find the device to use for a new multicast publisher for an IBRL user on `current_device_pk`.
///
/// Returns the current device if it has capacity. Otherwise returns the geographically
/// nearest activated device that has capacity, based on device Location coordinates.
/// Returns `None` if no device with capacity exists.
pub fn find_nearest_device_for_multicast<'a>(
    current_device_pk: &Pubkey,
    all_devices: &'a HashMap<Pubkey, DzDeviceInfo>,
) -> Option<&'a DzDeviceInfo> {
    let current = all_devices.get(current_device_pk)?;

    if device_has_multicast_publisher_capacity(current) {
        return Some(current);
    }

    // Current device is full — find nearest with capacity.
    all_devices
        .values()
        .filter(|d| d.pk != *current_device_pk && device_has_multicast_publisher_capacity(d))
        .min_by(|a, b| {
            let da = haversine_km(current.lat, current.lng, a.lat, a.lng);
            let db = haversine_km(current.lat, current.lng, b.lat, b.lng);
            da.partial_cmp(&db).unwrap_or(std::cmp::Ordering::Equal)
        })
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;

    fn make_device(pk: Pubkey, code: &str, lat: f64, lng: f64) -> DzDeviceInfo {
        DzDeviceInfo {
            pk,
            code: code.to_string(),
            lat,
            lng,
            users_count: 0,
            max_users: 100,
            reserved_seats: 0,
            multicast_publishers_count: 0,
            max_multicast_publishers: 0,
        }
    }

    fn make_full_device(pk: Pubkey, code: &str, lat: f64, lng: f64) -> DzDeviceInfo {
        DzDeviceInfo {
            users_count: 100,
            max_users: 100,
            ..make_device(pk, code, lat, lng)
        }
    }

    #[test]
    fn haversine_known_distance() {
        // Amsterdam → Frankfurt ≈ 365 km
        let km = haversine_km(52.37, 4.90, 50.11, 8.68);
        assert!((km - 365.0).abs() < 10.0, "expected ~365 km, got {km:.1}");
    }

    #[test]
    fn returns_current_device_when_available() {
        let pk = Pubkey::new_unique();
        let mut devices = HashMap::new();
        devices.insert(pk, make_device(pk, "ams", 52.37, 4.90));

        let result = find_nearest_device_for_multicast(&pk, &devices);
        assert_eq!(result.map(|d| d.pk), Some(pk));
    }

    #[test]
    fn falls_back_to_nearest_when_full() {
        let pk_ams = Pubkey::new_unique();
        let pk_fra = Pubkey::new_unique();
        let pk_nyc = Pubkey::new_unique();

        let mut devices = HashMap::new();
        devices.insert(pk_ams, make_full_device(pk_ams, "ams", 52.37, 4.90)); // full
        devices.insert(pk_fra, make_device(pk_fra, "fra", 50.11, 8.68)); // ~365 km from ams
        devices.insert(pk_nyc, make_device(pk_nyc, "nyc", 40.71, -74.01)); // ~5,860 km from ams

        let result = find_nearest_device_for_multicast(&pk_ams, &devices);
        assert_eq!(result.map(|d| d.pk), Some(pk_fra));
    }

    #[test]
    fn returns_none_when_all_full() {
        let pk = Pubkey::new_unique();
        let pk2 = Pubkey::new_unique();

        let mut devices = HashMap::new();
        devices.insert(pk, make_full_device(pk, "ams", 52.37, 4.90));
        devices.insert(pk2, make_full_device(pk2, "fra", 50.11, 8.68));

        let result = find_nearest_device_for_multicast(&pk, &devices);
        assert!(result.is_none());
    }

    #[test]
    fn returns_none_when_current_device_unknown() {
        let pk = Pubkey::new_unique();
        let unknown = Pubkey::new_unique();
        let mut devices = HashMap::new();
        devices.insert(pk, make_device(pk, "ams", 52.37, 4.90));

        let result = find_nearest_device_for_multicast(&unknown, &devices);
        assert!(result.is_none());
    }

    #[test]
    fn multicast_publisher_limit_respected() {
        let pk = Pubkey::new_unique();
        let device = DzDeviceInfo {
            multicast_publishers_count: 10,
            max_multicast_publishers: 10,
            ..make_device(pk, "ams", 52.37, 4.90)
        };
        assert!(!device_has_multicast_publisher_capacity(&device));

        // max_multicast_publishers == 0 means unlimited
        let unlimited = DzDeviceInfo {
            multicast_publishers_count: 10,
            max_multicast_publishers: 0,
            ..make_device(pk, "ams", 52.37, 4.90)
        };
        assert!(device_has_multicast_publisher_capacity(&unlimited));
    }
}
