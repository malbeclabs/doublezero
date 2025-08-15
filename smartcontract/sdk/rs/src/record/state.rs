pub use doublezero_record::state::RecordData;

pub fn read_record_data(data: &[u8]) -> Option<(&RecordData, &[u8])> {
    if data.len() < RecordData::WRITABLE_START_INDEX {
        return None;
    }

    let (header_data, body_data) = data.split_at(RecordData::WRITABLE_START_INDEX);
    let record_header = bytemuck::from_bytes::<RecordData>(header_data);
    Some((record_header, body_data))
}
