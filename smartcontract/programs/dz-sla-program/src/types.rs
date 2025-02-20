use ipnetwork::Ipv4Network;
use std::{net::Ipv4Addr, str::FromStr};

pub type IpV4 = [u8; 4]; // 4
pub type NetworkV4 = (IpV4, u8); // 5
pub type NetworkV4List = Vec<NetworkV4>; // 4 + len * 5

pub fn ipv4_parse(str: &str) -> IpV4 {
    Ipv4Addr::from_str(str).expect("Invalid IP").octets()
}

pub fn ipv4_to_string(ip: &IpV4) -> String {
    format!("{}.{}.{}.{}", ip[0], ip[1], ip[2], ip[3])
}

pub fn networkv4_parse(str: &str) -> NetworkV4 {
    let net = Ipv4Network::from_str(str).expect("Invalid IP");
    (net.ip().octets(), net.prefix())
}

/* 1.2.3.4/5, */
pub fn networkv4_list_parse(input: &str) -> NetworkV4List {
    let mut collected = Vec::new();

    // remove spaces
    let input = &input.replace(" ", "");

    input.split(',').for_each(|val| {
        collected.push(networkv4_parse(val));
    });

    collected
}

pub fn networkv4_to_string(net: &NetworkV4) -> String {
    format!(
        "{}.{}.{}.{}/{}",
        net.0[0], net.0[1], net.0[2], net.0[3], net.1
    )
}

pub fn networkv4_list_to_string(net_list: &NetworkV4List) -> String {
    net_list
        .iter()
        .map(networkv4_to_string)
        .collect::<Vec<String>>()
        .join(", ")
}

pub fn networkv4_to_ipnetwork(net: &NetworkV4) -> Ipv4Network {
    let ip_addr = Ipv4Addr::new(net.0[0], net.0[1], net.0[2], net.0[3]);
    Ipv4Network::new(ip_addr, net.1).expect("Invalid network")
}

pub fn bandwidth_parse(str: &str) -> u64 {
    let str = str.to_lowercase().replace(" ", "");
    let str = str.replace("gbps", "g");
    let str = str.replace("mbps", "m");
    let str = str.replace("kbps", "k");
    let str = str.replace("bps", "b");
    let mut unit = str.chars().last().unwrap_or('k');

    if !unit.is_alphabetic() {
        unit = 'k';
    }

    let str: String = str.chars().filter(|c| !c.is_alphabetic()).collect();

    let val = str.parse::<f64>().expect("Invalid bandwidth");

    match unit {
        'b' => val as u64,
        'k' => (val * 1000.0) as u64,
        'm' => (val * 1000000.0) as u64,
        'g' => (val * 1000000000.0) as u64,
        _ => (val * 1000.0) as u64,
    }
}

pub fn bandwidth_to_string(bandwidth: u64) -> String {
    if bandwidth < 1000 {
        format!("{}bps", bandwidth)
    } else if bandwidth < 1000000 {
        if bandwidth % 1000 == 0 {
            format!("{}Kbps", bandwidth / 1000)
        } else {
            format!("{:.2}Kbps", bandwidth as f64 / 1000.0)
        }
    } else if bandwidth < 1000000000 {
        if bandwidth % 1000000 == 0 {
            format!("{}Mbps", bandwidth / 1000000)
        } else {
            format!("{:.2}Mbps", bandwidth as f64 / 1000000.0)
        }
    } else if bandwidth % 1000000000 == 0 {
        format!("{}Gbps", bandwidth / 1000000000)
    } else {
        format!("{:.2}Gbps", bandwidth as f64 / 1000000000.0)
    }
}

pub fn delay_to_string(delay_ns: u64) -> String {
    let delay_ms = delay_ns as f64 / 1_000_000.0;
    format!("{:.2}ms", delay_ms)
}

pub fn jitter_to_string(delay_ns: u64) -> String {
    let delay_ms = delay_ns as f64 / 1_000_000.0;
    format!("{:.2}ms", delay_ms)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ipv4_parse() {
        let ip = ipv4_parse(&"1.2.3.4".to_string());
        assert_eq!(ip, [1, 2, 3, 4]);
    }

    #[test]
    fn test_ipv4_serialize() {
        let val = ipv4_parse(&"1.2.3.4".to_string());
        let data = borsh::to_vec(&val).unwrap();
        let val2 = borsh::from_slice::<IpV4>(&data).unwrap();
        assert_eq!(val, val2);
        assert_eq!(data.len(), 4);
    }

    #[test]
    fn test_networkv4_parse() {
        let ip = networkv4_parse(&"10.0.0.1/24".to_string());
        assert_eq!(ip, ([10, 0, 0, 1], 24));
    }

    #[test]
    fn test_networkv4_serialize() {
        let val = networkv4_parse(&"10.0.0.1/24".to_string());
        let data = borsh::to_vec(&val).unwrap();
        let val2 = borsh::from_slice::<NetworkV4>(&data).unwrap();
        assert_eq!(val, val2);
        assert_eq!(data.len(), 5);
    }

    #[test]
    fn test_networkv4_list_parse() {
        let ip = networkv4_list_parse(&"10.0.0.1/24,11.0.0.1/24".to_string());
        assert_eq!(ip, vec!(([10, 0, 0, 1], 24), ([11, 0, 0, 1], 24)));
    }

    #[test]
    fn test_networkv4_list_serialize() {
        let val = networkv4_list_parse(&"10.0.0.1/24,11.0.0.1/24".to_string());
        let data = borsh::to_vec(&val).unwrap();
        let val2 = borsh::from_slice::<NetworkV4List>(&data).unwrap();
        assert_eq!(val, val2);
        assert_eq!(data.len(), 4 + 5 + 5);
    }

    #[test]
    fn test_bandwidth_parse() {
        assert_eq!(bandwidth_parse("500"), 500000, "500");
        assert_eq!(bandwidth_parse("500bps"), 500, "500bps");
        assert_eq!(bandwidth_parse("1.50Kbps"), 1500, "1.50Kbps");
        assert_eq!(bandwidth_parse("2.50Mbps"), 2500000, "2.50Mbps");
        assert_eq!(bandwidth_parse("3.50Gbps"), 3500000000, "3.50Gbps");
    }

    #[test]
    fn test_bandwidth_to_string() {
        assert_eq!(bandwidth_to_string(500), "500bps", "500");
        assert_eq!(bandwidth_to_string(1500), "1.50Kbps", "1.50Kbps");
        assert_eq!(bandwidth_to_string(2500000), "2.50Mbps", "2.50Mbps");
        assert_eq!(bandwidth_to_string(3500000000), "3.50Gbps", "3.50Gbps");
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
