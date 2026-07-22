pub const MIN_COMPATIBLE_VERSION: &str = "0.21.0";

// First client release whose AccessPass decoder understands `EdgeSeat(Vec<FeedSeat>)` (#3954).
// Older decoders misparse every field after the variant tag and can abort on a bogus allowlist
// length, so EdgeSeat-typed passes may not be written while min_compatible_version still admits
// those clients (see `require_edge_seat_compatible_floor`).
pub const EDGE_SEAT_MIN_COMPATIBLE_VERSION: &str = "0.30.0";
