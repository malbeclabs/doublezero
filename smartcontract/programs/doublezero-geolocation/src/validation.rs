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
/// Rejects all non-globally-routable addresses including RFC 1918 private,
/// loopback, multicast, broadcast, link-local, shared address space (RFC 6598),
/// documentation/test ranges, benchmarking, protocol assignments, and reserved.
pub fn validate_public_ip(ip: &Ipv4Addr) -> Result<(), GeolocationError> {
    let octets = ip.octets();

    if ip.is_unspecified() {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // 0.0.0.0/8 "This network" (RFC 791)
    if octets[0] == 0 {
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

    // Shared Address Space: 100.64.0.0/10 (RFC 6598)
    if octets[0] == 100 && (64..=127).contains(&octets[1]) {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Link-local: 169.254.0.0/16
    if octets[0] == 169 && octets[1] == 254 {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Protocol Assignments: 192.0.0.0/24 (RFC 6890)
    if octets[0] == 192 && octets[1] == 0 && octets[2] == 0 {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Documentation: 192.0.2.0/24 TEST-NET-1 (RFC 5737)
    if octets[0] == 192 && octets[1] == 0 && octets[2] == 2 {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Benchmarking: 198.18.0.0/15 (RFC 2544)
    if octets[0] == 198 && (18..=19).contains(&octets[1]) {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Documentation: 198.51.100.0/24 TEST-NET-2 (RFC 5737)
    if octets[0] == 198 && octets[1] == 51 && octets[2] == 100 {
        return Err(GeolocationError::InvalidIpAddress);
    }

    // Documentation: 203.0.113.0/24 TEST-NET-3 (RFC 5737)
    if octets[0] == 203 && octets[1] == 0 && octets[2] == 113 {
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
            Ipv4Addr::new(185, 199, 108, 153),
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

    #[test]
    fn test_this_network_0_x() {
        let ip = Ipv4Addr::new(0, 1, 2, 3);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_shared_address_space() {
        let ips = [
            Ipv4Addr::new(100, 64, 0, 1),
            Ipv4Addr::new(100, 127, 255, 254),
        ];
        for ip in &ips {
            assert_eq!(
                validate_public_ip(ip),
                Err(GeolocationError::InvalidIpAddress),
                "expected {ip} to be rejected"
            );
        }
        // 100.63.x.x and 100.128.x.x should be valid
        assert!(validate_public_ip(&Ipv4Addr::new(100, 63, 255, 255)).is_ok());
        assert!(validate_public_ip(&Ipv4Addr::new(100, 128, 0, 0)).is_ok());
    }

    #[test]
    fn test_protocol_assignments() {
        let ip = Ipv4Addr::new(192, 0, 0, 1);
        assert_eq!(
            validate_public_ip(&ip),
            Err(GeolocationError::InvalidIpAddress)
        );
    }

    #[test]
    fn test_documentation_ranges() {
        let ips = [
            Ipv4Addr::new(192, 0, 2, 1),    // TEST-NET-1
            Ipv4Addr::new(198, 51, 100, 1), // TEST-NET-2
            Ipv4Addr::new(203, 0, 113, 1),  // TEST-NET-3
        ];
        for ip in &ips {
            assert_eq!(
                validate_public_ip(ip),
                Err(GeolocationError::InvalidIpAddress),
                "expected {ip} to be rejected"
            );
        }
    }

    #[test]
    fn test_benchmarking_range() {
        let ips = [
            Ipv4Addr::new(198, 18, 0, 1),
            Ipv4Addr::new(198, 19, 255, 254),
        ];
        for ip in &ips {
            assert_eq!(
                validate_public_ip(ip),
                Err(GeolocationError::InvalidIpAddress),
                "expected {ip} to be rejected"
            );
        }
        // 198.17.x.x and 198.20.x.x should be valid
        assert!(validate_public_ip(&Ipv4Addr::new(198, 17, 255, 255)).is_ok());
        assert!(validate_public_ip(&Ipv4Addr::new(198, 20, 0, 0)).is_ok());
    }
}
