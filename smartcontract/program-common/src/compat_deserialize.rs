use borsh::BorshDeserialize;
use std::io::{Cursor, Result as IoResult};

pub fn compat_deserialize<T: BorshDeserialize>(data: &[u8]) -> IoResult<T> {
    let mut cur = Cursor::new(data);
    T::deserialize_reader(&mut cur)
}

#[cfg(test)]
mod tests {
    use super::*;
    use borsh::{BorshDeserialize, BorshSerialize};
    use std::io::{ErrorKind, Result as IoResult};

    #[derive(Debug, PartialEq, BorshSerialize, BorshDeserialize)]
    struct FooV1 {
        a: u8,
    }

    #[derive(Debug, PartialEq, BorshSerialize, BorshDeserialize)]
    struct FooV2 {
        a: u8,
        b: u32,
    }

    #[derive(Debug, PartialEq, BorshSerialize, BorshDeserialize)]
    struct WithVecV1 {
        xs: Vec<u16>,
    }

    #[derive(Debug, PartialEq, BorshSerialize, BorshDeserialize)]
    struct WithVecV2 {
        xs: Vec<u16>,
        note: String,
    }

    fn is_decode_err<T>(r: &IoResult<T>) -> bool {
        matches!(r, Err(e) if matches!(e.kind(), ErrorKind::UnexpectedEof | ErrorKind::InvalidData))
    }

    #[test]
    fn test_forward_compat_allows_trailing() {
        let v2 = FooV2 {
            a: 7,
            b: 0xdeadbeef,
        };
        let buf = borsh::to_vec(&v2).unwrap();
        let v1: FooV1 = compat_deserialize(&buf).expect("should ignore extra fields");
        assert_eq!(v1, FooV1 { a: 7 });
    }

    #[test]
    fn test_insufficient_bytes_errors() {
        let v1 = FooV1 { a: 7 };
        let buf = borsh::to_vec(&v1).unwrap();
        let err: IoResult<FooV2> = compat_deserialize(&buf);
        assert!(is_decode_err(&err));
    }

    #[test]
    fn test_exact_match_ok() {
        let v1 = FooV1 { a: 42 };
        let buf = borsh::to_vec(&v1).unwrap();
        let got: FooV1 = compat_deserialize(&buf).unwrap();
        assert_eq!(got, v1);
    }

    #[test]
    fn test_empty_slice_errors() {
        let err: IoResult<FooV1> = compat_deserialize(&[]);
        assert!(is_decode_err(&err));
    }

    #[test]
    fn test_trailing_zeros_ignored() {
        let v1 = FooV1 { a: 1 };
        let mut buf = borsh::to_vec(&v1).unwrap();
        buf.extend([0u8; 16]);
        let got: FooV1 = compat_deserialize(&buf).unwrap();
        assert_eq!(got, v1);
    }

    #[test]
    fn test_tail_after_head_accounting() {
        let head = FooV1 { a: 9 };
        let head_bytes = borsh::to_vec(&head).unwrap();
        let tail = [0xAA, 0xBB, 0xCC];
        let mut buf = head_bytes.clone();
        buf.extend(tail);

        let got: FooV1 = compat_deserialize(&buf).unwrap();
        assert_eq!(got, head);

        let remainder = &buf[head_bytes.len()..];
        assert_eq!(remainder, &tail);
    }

    #[test]
    fn test_len_prefixed_types_plus_trailing_ok() {
        let v2 = WithVecV2 {
            xs: vec![1, 2, 3, 40000],
            note: "hello".to_string(),
        };
        let buf = borsh::to_vec(&v2).unwrap();
        let v1: WithVecV1 = compat_deserialize(&buf).unwrap();
        assert_eq!(
            v1,
            WithVecV1 {
                xs: vec![1, 2, 3, 40000]
            }
        );
    }

    #[test]
    fn test_insufficient_inside_len_prefixed_errors() {
        let mut buf = Vec::new();
        buf.extend((3u32).to_le_bytes()); // length=3
        buf.extend((123u16).to_le_bytes()); // only 1 element provided
        let err: IoResult<WithVecV1> = compat_deserialize(&buf);
        assert!(is_decode_err(&err));
    }
}
