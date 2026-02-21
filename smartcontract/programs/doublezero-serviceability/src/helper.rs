use crate::state::globalconfig::GlobalConfig;
use borsh::BorshDeserialize;
use solana_program::program_error::ProgramError;
use std::{
    fmt::{self},
    net::Ipv4Addr,
};

pub fn assign_bgp_community(globalconfig: &mut GlobalConfig) -> u16 {
    let assigned = globalconfig.next_bgp_community;
    globalconfig.next_bgp_community = assigned.saturating_add(1);
    assigned
}

pub fn format_option_displayable<T: fmt::Display>(opt: Option<T>) -> String {
    match opt {
        Some(value) => value.to_string(),
        None => "None".to_string(),
    }
}

#[macro_export]
macro_rules! format_option {
    ($opt:expr) => {
        format_option_displayable($opt)
    };
}

pub fn deserialize_vec_with_capacity<T: BorshDeserialize>(
    data: &mut &[u8],
) -> Result<Vec<T>, ProgramError> {
    // If the data doesn't contain enough bytes to read the vector size (4 bytes), return an empty vector.
    let len = u32::from_le_bytes(match data.get(..4) {
        Some(bytes) => match bytes.try_into() {
            Ok(arr) => arr,
            Err(_) => return Ok(Vec::new()),
        },
        None => return Ok(Vec::new()),
    });

    *data = &data[4..];
    let mut vec = Vec::with_capacity(len as usize + 1);
    for _ in 0..len {
        vec.push(T::deserialize(data)?);
    }
    Ok(vec)
}

/// Returns true if the given IPv4 address is globally routable (not a BGP
/// martian). Rejects all addresses that should never appear as a source in the
/// global routing table.
pub fn is_global(ip: Ipv4Addr) -> bool {
    let octets = ip.octets();

    // Reject ranges covered by std::net::Ipv4Addr built-in checks:
    //   is_unspecified: 0.0.0.0
    //   is_private:     10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
    //   is_loopback:    127.0.0.0/8
    //   is_link_local:  169.254.0.0/16
    //   is_broadcast:   255.255.255.255
    //   is_documentation: 192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24
    if ip.is_unspecified()
        || ip.is_private()
        || ip.is_loopback()
        || ip.is_link_local()
        || ip.is_broadcast()
        || ip.is_documentation()
    {
        return false;
    }

    // Additional BGP martian ranges not covered by std:

    // 0.0.0.0/8 — "this" network (RFC 791), is_unspecified only catches 0.0.0.0
    if octets[0] == 0 {
        return false;
    }
    // 100.64.0.0/10 — shared / CGNAT (RFC 6598)
    if octets[0] == 100 && (octets[1] & 0xC0) == 64 {
        return false;
    }
    // 192.0.0.0/24 — IETF protocol assignments (RFC 6890)
    if octets[0] == 192 && octets[1] == 0 && octets[2] == 0 {
        return false;
    }
    // 198.18.0.0/15 — benchmarking (RFC 2544)
    if octets[0] == 198 && (octets[1] & 0xFE) == 18 {
        return false;
    }
    // 224.0.0.0/4 — multicast (RFC 5771)
    if (octets[0] & 0xF0) == 224 {
        return false;
    }
    // 240.0.0.0/4 — reserved for future use (RFC 1112)
    if octets[0] >= 240 {
        return false;
    }

    true
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_deserialize_vec_with_capacity() {
        // Normal case
        let data = [3u8, 0, 0, 0, 10, 0, 0, 0, 20, 0, 0, 0, 30, 0, 0, 0];
        let result = deserialize_vec_with_capacity::<u32>(&mut &data[..]).unwrap();
        assert_eq!(result, vec![10, 20, 30]);

        // Error case: not enough data to read length
        let data = [0u8]; // Incomplete length
        let err = deserialize_vec_with_capacity::<u8>(&mut &data[..]).unwrap();
        assert_eq!(err, Vec::<u8>::new());
    }

    #[test]
    fn test_is_global() {
        // Publicly routable
        assert!(is_global(Ipv4Addr::new(1, 1, 1, 1)));
        assert!(is_global(Ipv4Addr::new(8, 8, 8, 8)));
        assert!(is_global(Ipv4Addr::new(100, 63, 255, 255))); // just below CGNAT
        assert!(is_global(Ipv4Addr::new(100, 128, 0, 0))); // just above CGNAT
        assert!(is_global(Ipv4Addr::new(172, 15, 255, 255))); // just below 172.16/12
        assert!(is_global(Ipv4Addr::new(172, 32, 0, 0))); // just above 172.16/12
        assert!(is_global(Ipv4Addr::new(198, 17, 255, 255))); // just below benchmarking
        assert!(is_global(Ipv4Addr::new(198, 20, 0, 0))); // just above benchmarking
        assert!(is_global(Ipv4Addr::new(203, 0, 114, 1))); // just above TEST-NET-3

        // BGP martians (should all be rejected)
        assert!(!is_global(Ipv4Addr::new(0, 0, 0, 0))); // unspecified
        assert!(!is_global(Ipv4Addr::new(0, 1, 2, 3))); // 0.0.0.0/8
        assert!(!is_global(Ipv4Addr::new(10, 0, 0, 1))); // private
        assert!(!is_global(Ipv4Addr::new(10, 255, 255, 255)));
        assert!(!is_global(Ipv4Addr::new(100, 64, 0, 1))); // CGNAT
        assert!(!is_global(Ipv4Addr::new(100, 127, 255, 255)));
        assert!(!is_global(Ipv4Addr::new(127, 0, 0, 1))); // loopback
        assert!(!is_global(Ipv4Addr::new(169, 254, 0, 1))); // link-local
        assert!(!is_global(Ipv4Addr::new(172, 16, 0, 1))); // private
        assert!(!is_global(Ipv4Addr::new(172, 31, 255, 255)));
        assert!(!is_global(Ipv4Addr::new(192, 0, 0, 1))); // IETF protocol assignments
        assert!(!is_global(Ipv4Addr::new(192, 0, 2, 1))); // TEST-NET-1
        assert!(!is_global(Ipv4Addr::new(192, 168, 0, 1))); // private
        assert!(!is_global(Ipv4Addr::new(198, 18, 0, 1))); // benchmarking
        assert!(!is_global(Ipv4Addr::new(198, 19, 0, 1)));
        assert!(!is_global(Ipv4Addr::new(198, 51, 100, 1))); // TEST-NET-2
        assert!(!is_global(Ipv4Addr::new(203, 0, 113, 1))); // TEST-NET-3
        assert!(!is_global(Ipv4Addr::new(224, 0, 0, 1))); // multicast
        assert!(!is_global(Ipv4Addr::new(239, 255, 255, 255)));
        assert!(!is_global(Ipv4Addr::new(240, 0, 0, 1))); // reserved
        assert!(!is_global(Ipv4Addr::new(255, 255, 255, 255))); // broadcast
    }
}

#[cfg(test)]
pub mod base_tests {
    use base64::{engine::general_purpose, Engine as _};
    use solana_sdk::program_error::ProgramError;

    pub fn test_parsing<T>(inputs: &[&str]) -> Result<(), ProgramError>
    where
        for<'a> T: TryFrom<&'a [u8]> + std::fmt::Debug,
        for<'a> <T as TryFrom<&'a [u8]>>::Error: std::fmt::Debug,
    {
        println!("\n{}", std::any::type_name::<T>());

        for (i, s) in inputs.iter().enumerate() {
            match general_purpose::STANDARD.decode(s) {
                Ok(bytes) => {
                    let slice: &[u8] = bytes.as_slice();
                    match T::try_from(slice) {
                        Ok(acc) => println!("{i}: ✅ OK {:?}", acc),
                        Err(e) => {
                            println!("{i}: Failed to parse: {:?}", e);
                            return Err(ProgramError::InvalidInstructionData);
                        }
                    }
                }
                Err(e) => {
                    println!("{i}: Base64 decode error: {:?}", e);
                    return Err(ProgramError::InvalidInstructionData);
                }
            }
        }
        Ok(())
    }
}
