use crate::types::{NetworkV4, NetworkV4List};
use solana_program::pubkey::Pubkey;
use std::mem::size_of;

pub struct ByteReader<'a> {
    data: &'a [u8],
    position: usize,
}

impl<'a> ByteReader<'a> {
    pub fn new(data: &'a [u8]) -> Self {
        Self { data, position: 0 }
    }

    pub fn has_no_space(&self, length: usize) -> bool {
        self.position + length > self.data.len()
    }

    pub fn read_enum<T: From<u8>>(&mut self) -> T {
        if self.has_no_space(size_of::<u8>()) {
            return T::from(0);
        }
        let value = u8::from_le_bytes(
            self.data[self.position..self.position + size_of::<u8>()]
                .try_into()
                .unwrap(),
        );
        self.position += size_of::<u8>();
        T::from(value)
    }

    #[allow(dead_code)]
    pub fn read_u8(&mut self) -> u8 {
        if self.has_no_space(size_of::<u8>()) {
            return u8::default();
        }
        let value = u8::from_le_bytes(
            self.data[self.position..self.position + size_of::<u8>()]
                .try_into()
                .unwrap(),
        );
        self.position += size_of::<u8>();
        value
    }

    pub fn read_u16(&mut self) -> u16 {
        if self.has_no_space(size_of::<u16>()) {
            return u16::default();
        }
        let value = u16::from_le_bytes(
            self.data[self.position..self.position + size_of::<u16>()]
                .try_into()
                .unwrap(),
        );
        self.position += size_of::<u16>();
        value
    }

    pub fn read_u32(&mut self) -> u32 {
        if self.has_no_space(size_of::<u32>()) {
            return u32::default();
        }
        let value = u32::from_le_bytes(
            self.data[self.position..self.position + size_of::<u32>()]
                .try_into()
                .unwrap(),
        );
        self.position += size_of::<u32>();
        value
    }

    pub fn read_u64(&mut self) -> u64 {
        if self.has_no_space(size_of::<u64>()) {
            return u64::default();
        }
        let value = u64::from_le_bytes(
            self.data[self.position..self.position + size_of::<u64>()]
                .try_into()
                .unwrap(),
        );
        self.position += size_of::<u64>();
        value
    }

    pub fn read_f64(&mut self) -> f64 {
        if self.has_no_space(size_of::<f64>()) {
            return f64::default();
        }
        let value = f64::from_le_bytes(
            self.data[self.position..self.position + size_of::<f64>()]
                .try_into()
                .unwrap(),
        );
        self.position += size_of::<f64>();
        value
    }

    pub fn read_u128(&mut self) -> u128 {
        if self.has_no_space(size_of::<u128>()) {
            return u128::default();
        }
        let value = u128::from_le_bytes(
            self.data[self.position..self.position + size_of::<u128>()]
                .try_into()
                .unwrap(),
        );
        self.position += size_of::<u128>();
        value
    }

    pub fn read_pubkey(&mut self) -> Pubkey {
        if self.has_no_space(size_of::<Pubkey>()) {
            return Pubkey::default();
        }
        let value =
            Pubkey::try_from(&self.data[self.position..self.position + size_of::<Pubkey>()])
                .unwrap_or_default();
        self.position += size_of::<Pubkey>();

        value
    }

    pub fn read_pubkey_vec(&mut self) -> Vec<Pubkey> {
        let mut list = Vec::new();

        let length = self.read_u32() as usize;
        if !self.has_no_space(length * 32) {
            for _ in 0..length {
                list.push(self.read_pubkey());
            }
        }

        list
    }

    pub fn read_ipv4(&mut self) -> std::net::Ipv4Addr {
        if self.has_no_space(size_of::<std::net::Ipv4Addr>()) {
            return std::net::Ipv4Addr::UNSPECIFIED;
        }

        let value = std::net::Ipv4Addr::new(
            self.data[self.position],
            self.data[self.position + 1],
            self.data[self.position + 2],
            self.data[self.position + 3],
        );
        self.position += size_of::<std::net::Ipv4Addr>();

        value
    }

    pub fn read_ipv4_list(&mut self) -> Vec<std::net::Ipv4Addr> {
        let mut list = Vec::<std::net::Ipv4Addr>::default();

        let length = self.read_u32() as usize;
        if !self.has_no_space(length * size_of::<std::net::Ipv4Addr>()) {
            for _ in 0..length {
                list.push(self.read_ipv4());
            }
        }

        list
    }

    pub fn read_networkv4(&mut self) -> NetworkV4 {
        if self.has_no_space(size_of::<NetworkV4>()) {
            return NetworkV4::default();
        }

        let ip = self.read_ipv4();
        let bits = self.read_u8();

        NetworkV4::new(ip, bits).unwrap_or_else(|_| NetworkV4::default())
    }

    pub fn read_networkv4_list(&mut self) -> NetworkV4List {
        let mut list = NetworkV4List::default();

        let length = self.read_u32() as usize;
        if !self.has_no_space(length * 5) {
            for _ in 0..length {
                list.push(self.read_networkv4());
            }
        }

        list
    }

    pub fn read_string(&mut self) -> String {
        let length = self.read_u32() as usize;
        if self.has_no_space(length) {
            return "".to_string();
        }
        let value =
            String::from_utf8_lossy(&self.data[self.position..self.position + length]).to_string();
        self.position += length;
        value
    }

    pub fn read_vec<T>(&mut self) -> Vec<T>
    where
        for<'z> T: From<&'z mut ByteReader<'a>>,
    {
        let length = self.read_u32() as usize;

        let mut vec = Vec::with_capacity(length);

        for _ in 0..length {
            vec.push(T::from(self));
        }
        vec
    }
}
