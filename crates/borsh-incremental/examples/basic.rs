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
