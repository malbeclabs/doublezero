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

    // Check if the current version is compatible with the required version
    pub fn warning(&self, client: &ProgramVersion) -> bool {
        self.major == client.major && self.minor == client.minor && self.patch > client.patch
    }

    // Check if the current version is incompatible with the required version
    pub fn error(&self, client: &ProgramVersion) -> bool {
        self.major > client.major || (self.major == client.major && self.minor > client.minor)
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
    fn test_program_version_warning1() {
        let program = ProgramVersion::new(1, 1, 3);
        let client = ProgramVersion::new(1, 2, 0);
        assert!(!program.warning(&client));
    }

    #[test]
    fn test_program_version_warning2() {
        let program = ProgramVersion::new(1, 2, 2);
        let client = ProgramVersion::new(1, 2, 3);
        assert!(!program.warning(&client));
    }

    #[test]
    fn test_program_version_warning3() {
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 2, 3);
        assert!(!program.warning(&client));
    }

    #[test]
    fn test_program_version_warning4() {
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 2, 2);
        assert!(program.warning(&client));
    }

    #[test]
    fn test_program_version_error1() {
        let program = ProgramVersion::new(1, 2, 3);
        let client = ProgramVersion::new(1, 3, 0);
        assert!(!program.error(&client));
    }

    #[test]
    fn test_program_version_error2() {
        let program = ProgramVersion::new(2, 0, 3);
        let client = ProgramVersion::new(1, 2, 0);
        assert!(program.error(&client));
    }

    #[test]
    fn test_program_version_error3() {
        let program = ProgramVersion::new(1, 3, 3);
        let client = ProgramVersion::new(1, 2, 0);
        assert!(program.error(&client));
    }

    #[test]
    fn test_program_version_error4() {
        let program = ProgramVersion::new(1, 0, 3);
        let client = ProgramVersion::new(2, 2, 0);
        assert!(!program.error(&client));
    }
}
