use borsh::BorshSerialize;
use core::fmt;

#[derive(BorshSerialize, Debug, PartialEq, Clone)]
pub struct ProgramVersion {
    pub mayor: u32,
    pub minor: u32,
    pub patch: u32,
}

impl fmt::Display for ProgramVersion {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}.{}.{}", self.mayor, self.minor, self.patch)
    }
}

impl ProgramVersion {
    pub fn new(mayor: u32, minor: u32, patch: u32) -> Self {
        Self {
            mayor,
            minor,
            patch,
        }
    }

    #[cfg(not(test))]
    pub fn get_cargo_pkg_version() -> Self {
        Self {
            mayor: env!("CARGO_PKG_VERSION_MAJOR").parse().unwrap_or(0),
            minor: env!("CARGO_PKG_VERSION_MINOR").parse().unwrap_or(0),
            patch: env!("CARGO_PKG_VERSION_PATCH").parse().unwrap_or(0),
        }
    }
    #[cfg(test)]
    pub fn get_cargo_pkg_version() -> Self {
        Self {
            mayor: 100,
            minor: 100,
            patch: 100,
        }
    }

    // Check if the current version is compatible with the required version
    pub fn warning(&self, required: &ProgramVersion) -> bool {
        self.mayor == required.mayor && self.minor == required.minor && self.patch < required.patch
    }

    // Check if the current version is incompatible with the required version
    pub fn error(&self, required: &ProgramVersion) -> bool {
        self.mayor < required.mayor || (self.mayor == required.mayor && self.minor < required.minor)
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
    fn test_program_version_warning() {
        let current = ProgramVersion::new(1, 2, 3);
        let required = ProgramVersion::new(1, 1, 0);
        assert!(!current.warning(&required));

        let current = ProgramVersion::new(1, 2, 3);
        let required = ProgramVersion::new(1, 2, 2);
        assert!(!current.warning(&required));

        let current = ProgramVersion::new(1, 2, 3);
        let required = ProgramVersion::new(1, 2, 3);
        assert!(!current.warning(&required));

        let current = ProgramVersion::new(1, 2, 3);
        let required = ProgramVersion::new(1, 2, 4);
        assert!(current.warning(&required));
    }

    #[test]
    fn test_program_version_error() {
        let current = ProgramVersion::new(1, 3, 3);
        let required = ProgramVersion::new(1, 2, 0);
        assert!(!current.error(&required));

        let current = ProgramVersion::new(2, 2, 3);
        let required = ProgramVersion::new(1, 0, 0);
        assert!(!current.error(&required));

        let current = ProgramVersion::new(1, 2, 3);
        let required = ProgramVersion::new(1, 3, 0);
        assert!(current.error(&required));

        let current = ProgramVersion::new(1, 2, 3);
        let required = ProgramVersion::new(2, 0, 0);
        assert!(current.error(&required));
    }
}
