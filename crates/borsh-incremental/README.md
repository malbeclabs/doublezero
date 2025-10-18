# borsh-incremental

Incremental Borsh deserialization with defaults.

This derive macro lets you safely decode incomplete or evolving data structures by applying `Default` values (or custom expressions) to missing fields — ideal for backward-compatible schema evolution or partially initialized onchain accounts.

## Example

```rust
use borsh::{to_vec, BorshSerialize};
use borsh_incremental::BorshDeserializeIncremental;

#[derive(BorshSerialize, BorshDeserializeIncremental, Debug)]
struct ExampleData {
    #[incremental(default = "Unnamed".to_string())]
    name: String,
    count: u32,
}

fn main() {
    let data = ExampleData {
        name: "Alice".into(),
        count: 5,
    };
    let bytes = to_vec(&data).unwrap();
    let parsed = ExampleData::try_from(&bytes[..]).unwrap();
    println!("{:?}", parsed);
}
```

## Attributes

| Attribute                       | Applies To | Description                                                                        |
| ------------------------------- | ---------- | ---------------------------------------------------------------------------------- |
| `#[incremental(error = <Path>)]`    | struct     | Sets the error type for `TryFrom<&[u8]>`.                                          |
| `#[incremental(default = <Expr>)]`  | field      | Expression or value used if field cannot be deserialized.                          |
| `#[incremental(deser_with = <fn>)]` | field      | Uses a custom function `fn(&mut &[u8]) -> Result<T, E>` to deserialize that field. |

## Features

- Implements `TryFrom<&[u8]>` for your struct
- Automatically fills missing or invalid fields with defaults
- Supports per-field custom deserializers
- Ideal for forward/backward compatibility or partial account decoding
