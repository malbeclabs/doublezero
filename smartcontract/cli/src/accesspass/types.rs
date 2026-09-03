use clap::ValueEnum;
use doublezero_serviceability::state::accesspass::AccessPassKind;

/// The `--type` flag's values. One per `AccessPassType` variant. Kept next to the access pass
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
