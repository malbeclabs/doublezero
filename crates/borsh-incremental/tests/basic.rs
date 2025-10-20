use borsh::{to_vec, BorshSerialize};
use borsh_incremental::BorshDeserializeIncremental;

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq)]
#[incremental(error = std::io::Error)]
struct Simple {
    #[incremental(default = 42)]
    a: u32,
    #[incremental(default = 7)]
    b: u32,
}

#[test]
fn test_incremental_deser() {
    let data = Simple { a: 1, b: 2 };
    let bytes = to_vec(&data).unwrap();
    let decoded = Simple::try_from(&bytes[..]).unwrap();
    assert_eq!(decoded, data);
}

#[test]
fn test_truncated_input_defaults_used() {
    #[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq)]
    #[incremental(error = std::io::Error)]
    struct T {
        #[incremental(default = 10)]
        a: u32,
        #[incremental(default = 20)]
        b: u32,
        #[incremental(default = 30)]
        c: u32,
    }

    let data = T { a: 1, b: 2, c: 3 };
    let bytes = borsh::to_vec(&data).unwrap();

    // Truncate halfway through serialized data
    let truncated = &bytes[..4]; // only enough for `a`
    let decoded = T::try_from(truncated).unwrap();
    assert_eq!(decoded, T { a: 1, b: 20, c: 30 });
}

#[test]
fn test_custom_deser_with() {
    #[derive(BorshDeserializeIncremental, Debug, PartialEq)]
    #[incremental(error = std::io::Error)]
    struct U {
        #[incremental(deser_with = read_bool)]
        x: bool,
        #[incremental(default = 99)]
        y: u8,
    }

    let buf = [1u8]; // only one byte
    let decoded = U::try_from(&buf[..]).unwrap();
    assert_eq!(decoded, U { x: true, y: 99 });
}

#[test]
fn test_empty_input_all_defaults() {
    #[derive(BorshDeserializeIncremental, Debug, PartialEq)]
    #[incremental(error = std::io::Error)]
    struct V {
        #[incremental(default = "hi".to_string())]
        s: String,
        #[incremental(default = 123)]
        n: u32,
    }

    let decoded = V::try_from(&[][..]).unwrap();
    assert_eq!(
        decoded,
        V {
            s: "hi".into(),
            n: 123
        }
    );
}

#[test]
fn test_plain_fields_no_attrs() {
    #[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq)]
    #[incremental(error = std::io::Error)]
    struct W {
        a: u8,
        b: u8,
    }

    let data = W { a: 5, b: 6 };
    let bytes = borsh::to_vec(&data).unwrap();
    let decoded = W::try_from(&bytes[..1]).unwrap(); // only `a` present
    assert_eq!(decoded, W { a: 5, b: 0 }); // b defaulted
}

fn read_bool(data: &mut &[u8]) -> Result<bool, std::io::Error> {
    if data.is_empty() {
        return Ok(false);
    }
    let b = data[0] != 0;
    *data = &data[1..]; // advance slice
    Ok(b)
}

#[test]
fn test_backward_compat_v1_to_v2() {
    use borsh::BorshSerialize;

    #[derive(BorshSerialize)]
    struct ExampleV1 {
        name: String,
        count: u32,
    }

    #[derive(BorshDeserializeIncremental, Debug, PartialEq)]
    // no explicit error → defaults to std::io::Error
    struct ExampleV2 {
        #[incremental(default = "Unnamed".to_string())]
        name: String,
        count: u32,
        #[incremental(default = "active".to_string())]
        status: String, // newly added field in V2
    }

    let old = ExampleV1 {
        name: "Alice".into(),
        count: 5,
    };
    let old_bytes = borsh::to_vec(&old).unwrap();

    let v2 = ExampleV2::try_from(&old_bytes[..]).unwrap();
    assert_eq!(
        v2,
        ExampleV2 {
            name: "Alice".into(),
            count: 5,
            status: "active".into()
        }
    );
}

#[test]
fn test_truncated_input_v2_defaults() {
    use borsh::BorshSerialize;

    #[derive(BorshSerialize)]
    struct ExampleV1 {
        name: String,
        count: u32,
    }

    #[derive(BorshDeserializeIncremental, Debug, PartialEq)]
    struct ExampleV2 {
        #[incremental(default = "Unnamed".to_string())]
        name: String,
        count: u32,
        #[incremental(default = "active".to_string())]
        status: String,
    }

    let old = ExampleV1 {
        name: "Alice".into(),
        count: 5,
    };
    let old_bytes = borsh::to_vec(&old).unwrap();

    // Keep only the fully-encoded `name`, drop the 4-byte `count`.
    // V2 should default `count` to 0 and `status` to "active".
    let truncated_after_name = &old_bytes[..old_bytes.len() - 4];
    let v2 = ExampleV2::try_from(truncated_after_name).unwrap();

    assert_eq!(
        v2,
        ExampleV2 {
            name: "Alice".into(),
            count: 0,
            status: "active".into()
        }
    );
}

#[test]
fn test_truncated_input_v2_partial_field_errors() {
    use borsh::BorshSerialize;

    #[derive(BorshSerialize)]
    struct ExampleV1 {
        name: String,
        count: u32,
    }

    #[derive(BorshDeserializeIncremental, Debug, PartialEq)]
    struct ExampleV2 {
        #[incremental(default = "Unnamed".to_string())]
        name: String,
        count: u32,
        #[incremental(default = "active".to_string())]
        status: String,
    }

    let old = ExampleV1 {
        name: "Alice".into(),
        count: 5,
    };
    let old_bytes = borsh::to_vec(&old).unwrap();

    // Truncate *inside* the first field's payload (after reading the 4-byte length).
    // "Alice" => len=5; keep 4 (len) + 2 (partial payload) = 6 bytes total.
    let truncated = &old_bytes[..(4 + 2)];
    let err = ExampleV2::try_from(truncated).unwrap_err();

    // Borsh may surface either UnexpectedEof or InvalidData here; accept either.
    let k = err.kind();
    assert!(
        k == std::io::ErrorKind::UnexpectedEof || k == std::io::ErrorKind::InvalidData,
        "got {:?}",
        k
    );
}

#[test]
fn test_default_error_type_is_io_error() {
    #[derive(BorshDeserializeIncremental, Debug, PartialEq)]
    struct E {
        #[incremental(default = 1)]
        x: u8,
    }

    let e = E::try_from(&[][..]).unwrap();
    assert_eq!(e.x, 1);
}

#[test]
fn test_custom_deser_with_consumes_then_defaults() {
    fn read_one(data: &mut &[u8]) -> Result<u8, std::io::Error> {
        let v = data.first().copied().unwrap_or(0);
        if !data.is_empty() {
            *data = &data[1..];
        }
        Ok(v)
    }

    #[derive(BorshDeserializeIncremental, Debug, PartialEq)]
    struct C {
        #[incremental(deser_with = read_one)]
        head: u8,
        #[incremental(default = 77)]
        tail: u8,
    }

    let buf = [9u8]; // only enough for head
    let c = C::try_from(&buf[..]).unwrap();
    assert_eq!(c, C { head: 9, tail: 77 });
}

#[test]
fn test_trailing_bytes_ignored() {
    #[derive(BorshSerialize, BorshDeserializeIncremental, Debug, PartialEq)]
    struct Z {
        #[incremental(default = 1)]
        x: u8,
    }

    let bytes = [42u8, 99, 100, 101]; // 42 is `x`, rest are extra
    let z = Z::try_from(&bytes[..]).unwrap();
    assert_eq!(z, Z { x: 42 });
}

#[test]
fn test_option_field_defaults_to_none() {
    #[derive(BorshDeserializeIncremental, Debug, PartialEq)]
    struct Opty {
        v: Option<u32>,
    } // no #[incremental(default = ...)]

    let o = Opty::try_from(&[][..]).unwrap();
    assert_eq!(o, Opty { v: None });
}
