use clap::ValueEnum;
use doublezero_serviceability::state::accesspass::AccessPassKind;

/// The `--accesspass-type` flag's values. One per `AccessPassType` variant. Kept next to the access pass
/// commands because `access-pass close` and `user delete` both take it.
#[derive(Debug, Clone, Copy, PartialEq, Eq, ValueEnum)]
pub enum CliAccessPassType {
    Prepaid,
    SolanaValidator,
    SolanaRPC,
    Others,
    EdgeSeat,
}

impl From<CliAccessPassType> for AccessPassKind {
    fn from(value: CliAccessPassType) -> Self {
        match value {
            CliAccessPassType::Prepaid => AccessPassKind::Prepaid,
            CliAccessPassType::SolanaValidator => AccessPassKind::SolanaValidator,
            CliAccessPassType::SolanaRPC => AccessPassKind::SolanaRPC,
            CliAccessPassType::Others => AccessPassKind::Others,
            CliAccessPassType::EdgeSeat => AccessPassKind::EdgeSeat,
        }
    }
}

impl From<AccessPassKind> for CliAccessPassType {
    fn from(value: AccessPassKind) -> Self {
        match value {
            AccessPassKind::Prepaid => CliAccessPassType::Prepaid,
            AccessPassKind::SolanaValidator => CliAccessPassType::SolanaValidator,
            AccessPassKind::SolanaRPC => CliAccessPassType::SolanaRPC,
            AccessPassKind::Others => CliAccessPassType::Others,
            AccessPassKind::EdgeSeat => CliAccessPassType::EdgeSeat,
        }
    }
}

/// Prints the value as `--accesspass-type` spells it, so an operator can paste it into the flag.
impl std::fmt::Display for CliAccessPassType {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let value = self
            .to_possible_value()
            .expect("every CliAccessPassType variant has a value");
        f.write_str(value.get_name())
    }
}
