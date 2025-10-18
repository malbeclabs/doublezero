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
    let incremental = &bytes[..4]; // only enough for `a`
    let decoded = T::try_from(incremental).unwrap();
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
