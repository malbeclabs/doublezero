#[cfg(test)]
mod tests {
    use super::*;

    type Ipv4AddrOld = [u8; 4];
    type NetworkV4Old = (Ipv4AddrOld, u8);

    #[derive(BorshSerialize)]
    struct TestStructOld {
        ip: Ipv4AddrOld,
        network: NetworkV4Old,
    }

    #[derive(BorshSerialize)]
    struct TestStructNew {
        ip: Ipv4Addr,
        network: NetworkV4,
    }

    #[test]
    fn test_serialization_of_ipv4_types() {
        let mut buf1: Vec<u8> = Vec::new();
        let mut buf2: Vec<u8> = Vec::new();

        let d1 = TestStructOld {
            ip: "1.2.3.4".parse::<Ipv4Addr>().unwrap().octets(),
            network: ("10.11.12.13".parse::<Ipv4Addr>().unwrap().octets(), 24),
        };

        let d2 = TestStructNew {
            ip: "1.2.3.4".parse().unwrap(),
            network: "10.11.12.13/24".parse().unwrap(),
        };

        d1.serialize(&mut buf1).expect("serialization failed");
        d2.serialize(&mut buf2).expect("serialization failed");

        assert_eq!(buf1, buf2, "Serialized data does not match");
    }

    #[test]
    fn test_bandwidth_parse() {
        assert_eq!(bandwidth_parse("500").unwrap(), 500000, "500");
        assert_eq!(bandwidth_parse("500bps").unwrap(), 500, "500bps");
        assert_eq!(bandwidth_parse("1.50Kbps").unwrap(), 1500, "1.50Kbps");
        assert_eq!(bandwidth_parse("2.50Mbps").unwrap(), 2500000, "2.50Mbps");
        assert_eq!(bandwidth_parse("3.50Gbps").unwrap(), 3500000000, "3.50Gbps");
    }

    #[test]
    fn test_bandwidth_to_string() {
        assert_eq!(bandwidth_to_string(&500), "500bps", "500");
        assert_eq!(bandwidth_to_string(&1500), "1.50Kbps", "1.50Kbps");
        assert_eq!(bandwidth_to_string(&2500000), "2.50Mbps", "2.50Mbps");
        assert_eq!(bandwidth_to_string(&3500000000), "3.50Gbps", "3.50Gbps");
    }

    #[test]
    fn test_delay_to_string() {
        assert_eq!(delay_to_string(1_000_000), "1.00ms");
        assert_eq!(delay_to_string(1_500_000), "1.50ms");
        assert_eq!(delay_to_string(1_000_000_000), "1000.00ms");
    }

    #[test]
    fn test_jitter_to_string() {
        assert_eq!(jitter_to_string(1_000_000), "1.00ms");
        assert_eq!(jitter_to_string(1_500_000), "1.50ms");
        assert_eq!(jitter_to_string(1_000_000_000), "1000.00ms");
    }
}
