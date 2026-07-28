// Excludes every client predating the `EdgeSeat(Vec<FeedSeat>)` AccessPass decoder (#3954,
// #4030), which misparses each field after the variant tag and can abort on the bogus
// allowlist length that follows. testnet already holds an EdgeSeat pass, so this is live.
//
// 0.30.0 is the lowest safe floor and therefore also the widest: `client/v0.30.0` was tagged
// before its version-bump commit merged, so its binaries self-report 0.29.0 -- the same as the
// genuine v0.29.0 release, which has no EdgeSeat decoder. Admitting one admits the other, so
// the oldest release this floor admits is v0.31.0 (self-reports 0.30.0). Tagging was fixed for
// v0.32.0 onward in #4068.
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
