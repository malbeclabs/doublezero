// v0.30.0 is the first release whose `AccessPass` decoder understands
// `EdgeSeat(Vec<FeedSeat>)` (#3954, #4030); older clients misparse every field after the
// variant tag.
pub const MIN_COMPATIBLE_VERSION: &str = "0.30.0";

#[cfg(test)]
mod tests {
    use super::MIN_COMPATIBLE_VERSION;
    use crate::programversion::ProgramVersion;
    use std::str::FromStr;

    /// `process_initialize_global_state` stamps this const onto `ProgramConfig` with an
    /// `unwrap()`, and `SetMinVersion` rejects a floor above `ProgramConfig.version`. A floor
    /// above the program's own version would also reject the client shipping alongside it.
    #[test]
    fn min_compatible_version_parses_and_does_not_exceed_current() {
        let min = ProgramVersion::from_str(MIN_COMPATIBLE_VERSION)
            .expect("MIN_COMPATIBLE_VERSION must parse as major.minor.patch");
        assert!(
            min <= ProgramVersion::current(),
            "MIN_COMPATIBLE_VERSION {min} exceeds the program version {}",
            ProgramVersion::current()
        );
    }
}
