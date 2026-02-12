use std::net::Ipv4Addr;

use crate::error::GeolocationError;

pub const MAX_CODE_LENGTH: usize = 32;

pub fn validate_code_length(code: &str) -> Result<(), GeolocationError> {
    if code.is_empty() || code.len() > MAX_CODE_LENGTH {
        return Err(GeolocationError::InvalidCodeLength);
    }
    Ok(())
}

/// Validates that the given IPv4 address is publicly routable.
/// Rejects:
/// - Private ranges: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
/// - Loopback: 127.0.0.0/8
/// - Multicast: 224.0.0.0/4
/// - Broadcast: 255.255.255.255
/// - Link-local: 169.254.0.0/16
/// - Unspecified: 0.0.0.0
/// - Reserved: 240.0.0.0/4
pub fn validate_public_ip(ip: &Ipv4Addr) -> Result<(), GeolocationError> {
    let octets = ip.octets();

    if ip.is_unspecified() {
        return Err(GeolocationError::InvalidIpAddress);
    }

    if ip.is_loopback() {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Private: 10.0.0.0/8
    if octets[0] == 10 {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Private: 172.16.0.0/12
    if octets[0] == 172 && (16..=31).contains(&octets[1]) {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Private: 192.168.0.0/16
    if octets[0] == 192 && octets[1] == 168 {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Link-local: 169.254.0.0/16
    if octets[0] == 169 && octets[1] == 254 {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Multicast: 224.0.0.0/4 (224-239.x.x.x)
    if (224..=239).contains(&octets[0]) {
        return Err(GeolocationError::InvalidIpAddress);
    }

    if ip.is_broadcast() {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Reserved: 240-254.x.x.x (future use)
    if octets[0] >= 240 {
        return Err(GeolocationError::InvalidIpAddress);
    }

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_validate_code_length_empty() {
        assert_eq!(
            validate_code_length(""),
            Err(GeolocationError::InvalidCodeLength)
        );
    }

    #[test]
    fn test_validate_code_length_max() {
        let code = "a".repeat(MAX_CODE_LENGTH);
        assert!(validate_code_length(&code).is_ok());
    }

    #[test]
    fn test_validate_code_length_exceeds_max() {
        let code = "a".repeat(MAX_CODE_LENGTH + 1);
        assert_eq!(
            validate_code_length(&code),
            Err(GeolocationError::InvalidCodeLength)
        );
    }

    #[test]
    fn test_valid_public_ips() {
        let valid_ips = [
            Ipv4Addr::new(8, 8, 8, 8),
            Ipv4Addr::new(1, 1, 1, 1),
            Ipv4Addr::new(203, 0, 113, 42),
        ];
        for ip in &valid_ips {
            assert!(validate_public_ip(ip).is_ok(), "expected {ip} to be valid");
        }
    }

    #[test]
    fn test_private_10_network() {
        let ip = Ipv4Addr::new(10, 0, 0, 1);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_private_172_network() {
        let ip = Ipv4Addr::new(172, 16, 0, 1);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_private_192_168_network() {
        let ip = Ipv4Addr::new(192, 168, 1, 1);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_loopback() {
        let ip = Ipv4Addr::new(127, 0, 0, 1);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_multicast_low() {
        let ip = Ipv4Addr::new(224, 0, 0, 1);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_multicast_high() {
        let ip = Ipv4Addr::new(239, 255, 255, 255);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_broadcast() {
        let ip = Ipv4Addr::new(255, 255, 255, 255);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_link_local() {
        let ip = Ipv4Addr::new(169, 254, 1, 1);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_unspecified() {
        let ip = Ipv4Addr::new(0, 0, 0, 0);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_reserved_range() {
        let ip = Ipv4Addr::new(240, 0, 0, 1);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }
}
