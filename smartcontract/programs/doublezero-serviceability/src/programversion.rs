use borsh::BorshSerialize;
use core::fmt;
use std::str::FromStr;

#[derive(BorshSerialize, Debug, PartialEq, Clone, Default)]
pub struct ProgramVersion {
    pub major: u32,
    pub minor: u32,
    pub patch: u32,
}

impl fmt::Display for ProgramVersion {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}.{}.{}", self.major, self.minor, self.patch)
    }
}

impl FromStr for ProgramVersion {
    type Err = &'static str;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let parts: Vec<&str> = s.split('.').collect();
        if parts.len() != 3 {
            return Err("Invalid version format");
        }

        let major = parts[0]
            .parse::<u32>()
            .map_err(|_| "Invalid major version")?;
        let minor = parts[1]
            .parse::<u32>()
            .map_err(|_| "Invalid minor version")?;
        let patch = parts[2]
            .parse::<u32>()
            .map_err(|_| "Invalid patch version")?;

        Ok(ProgramVersion::new(major, minor, patch))
    }
}

impl ProgramVersion {
    pub fn new(major: u32, minor: u32, patch: u32) -> Self {
        Self {
            major,
            minor,
            patch,
        }
    }

    pub fn current() -> Self {
        Self {
            major: env!("CARGO_PKG_VERSION_MAJOR").parse().unwrap_or_default(),
            minor: env!("CARGO_PKG_VERSION_MINOR").parse().unwrap_or_default(),
            patch: env!("CARGO_PKG_VERSION_PATCH").parse().unwrap_or_default(),
        }
    }

    // Check if there's a patch version difference (warning level)
    // Returns true if major and minor match but patch differs
    pub fn has_patch_mismatch(&self, client: &ProgramVersion) -> bool {
        self.major == client.major && self.minor == client.minor && self.patch != client.patch
    }

    // Check if there's a minor version difference (warning level)
    // Returns true if major matches but minor differs
    pub fn has_minor_mismatch(&self, client: &ProgramVersion) -> bool {
        self.major == client.major && self.minor != client.minor
    }

    // Check if there's a major version difference (error level)
    // Returns true if major versions differ
    pub fn has_major_mismatch(&self, client: &ProgramVersion) -> bool {
        self.major != client.major
    }

    // Legacy methods for backward compatibility
    pub fn warning(&self, client: &ProgramVersion) -> bool {
        // Warning when program version is newer than client version (client is out of date)
        (self.has_minor_mismatch(client) && self.minor > client.minor)
            || (self.has_patch_mismatch(client) && self.patch > client.patch)
    }

    pub fn error(&self, client: &ProgramVersion) -> bool {
        // Error only on major version mismatches
        self.has_major_mismatch(client)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_program_version_display() {
        let version = ProgramVersion::new(1, 2, 3);
        assert_eq!(version.to_string(), "1.2.3");
    }

    #[test]
    fn test_has_patch_mismatch() {
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 2, 4);
        assert!(program.has_patch_mismatch(&client));

        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 3, 3);
        assert!(!program.has_patch_mismatch(&client));
    }

    #[test]
    fn test_has_minor_mismatch() {
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 3, 3);
        assert!(program.has_minor_mismatch(&client));

        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(2, 2, 3);
        assert!(!program.has_minor_mismatch(&client));
    }

    #[test]
    fn test_has_major_mismatch() {
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(2, 2, 3);
        assert!(program.has_major_mismatch(&client));

        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 3, 3);
        assert!(!program.has_major_mismatch(&client));
    }

    #[test]
    fn test_warning() {
        // Program newer patch - warning (client out of date)
        let program = ProgramVersion::new(1, 2, 4);
        let client = ProgramVersion::new(1, 2, 3);
        assert!(program.warning(&client));

        // Program newer minor - warning (client out of date)
        let program = ProgramVersion::new(1, 3, 3);
        let client = ProgramVersion::new(1, 2, 3);
        assert!(program.warning(&client));

        // Client newer patch - no warning (program out of date)
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 2, 4);
        assert!(!program.warning(&client));

        // Client newer minor - no warning (program out of date)
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 3, 3);
        assert!(!program.warning(&client));

        // Major mismatch - not a warning (should be error)
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(2, 2, 3);
        assert!(!program.warning(&client));
    }

    #[test]
    fn test_error() {
        // Major mismatch - error
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(2, 2, 3);
        assert!(program.error(&client));

        // Different major (program higher) - error
        let program = ProgramVersion::new(2, 2, 3);
        let client = ProgramVersion::new(1, 2, 3);
        assert!(program.error(&client));

        // Same major, client newer - no error (should be warning)
        let program = ProgramVersion::new(1, 2, 2);
        let client = ProgramVersion::new(1, 2, 3);
        assert!(!program.error(&client));

        // Same major, program newer - no error
        let program = ProgramVersion::new(1, 2, 4);
        let client = ProgramVersion::new(1, 2, 3);
        assert!(!program.error(&client));

        // Same version - no error
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 2, 3);
        assert!(!program.error(&client));
    }
}
