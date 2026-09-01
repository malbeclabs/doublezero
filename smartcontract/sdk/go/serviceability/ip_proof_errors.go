package serviceability

import (
	"errors"
	"fmt"
)

// ProgramError is a doublezero-serviceability custom program error, identified by
// the code the program returns through `InstructionError: {Custom: N}`.
//
// It is a comparable value type, so the named sentinels below match with
// errors.Is once a transaction error has been annotated by
// ClassifyProgramError.
type ProgramError struct {
	Code uint32
}

func (e ProgramError) Error() string {
	return fmt.Sprintf("%s (custom program error %d)", ProgramErrorMessage(e.Code), e.Code)
}

// The RFC-27 IP-ownership-proof rejection classes (codes 105-118), named so a
// caller can tell a missing proof from a stale one from a rotated verifier key
// instead of reading a bare error code out of an RPC blob.
//
// See rfcs/rfc27-ip-verification.md and the DoubleZeroError enum in
// smartcontract/programs/doublezero-serviceability/src/error.rs.
var (
	ErrIPOwnershipProofRequired         = ProgramError{Code: 105}
	ErrIPVerifierNotConfigured          = ProgramError{Code: 106}
	ErrIPProofPayerMismatch             = ProgramError{Code: 107}
	ErrIPProofClientIPMismatch          = ProgramError{Code: 108}
	ErrIPProofUserTypeMismatch          = ProgramError{Code: 109}
	ErrIPProofEpochOutOfWindow          = ProgramError{Code: 110}
	ErrIPProofInstructionsSysvarMissing = ProgramError{Code: 111}
	ErrIPProofEd25519InstructionMissing = ProgramError{Code: 112}
	ErrIPProofEd25519OffsetsInvalid     = ProgramError{Code: 113}
	ErrIPProofSignatureCountInvalid     = ProgramError{Code: 114}
	ErrIPProofVerifierKeyMismatch       = ProgramError{Code: 115}
	ErrIPProofSignatureMismatch         = ProgramError{Code: 116}
	ErrIPProofMessageMismatch           = ProgramError{Code: 117}
	ErrIPProofVersionUnsupported        = ProgramError{Code: 118}
)

// classifiedProgramError carries the named ProgramError alongside the original
// transaction error, so errors.Is matches either one and the RPC detail is not
// thrown away.
type classifiedProgramError struct {
	named ProgramError
	cause error
}

func (e *classifiedProgramError) Error() string { return formatRPCError(e.cause) }

func (e *classifiedProgramError) Unwrap() []error { return []error{e.named, e.cause} }

// ClassifyProgramError annotates a transaction error with the named
// ProgramError for its custom error code, so a caller can match it with
// errors.Is (e.g. against ErrIPOwnershipProofRequired) and an operator reading
// the message sees the error name rather than a raw JSON-RPC blob.
//
// Returns err unchanged when it carries no serviceability custom error code.
func ClassifyProgramError(err error) error {
	if err == nil {
		return nil
	}
	code, ok := parseCustomErrorCode(err)
	if !ok {
		return err
	}
	return &classifiedProgramError{named: ProgramError{Code: code}, cause: err}
}

// AsProgramError reports the serviceability custom error code carried by err,
// if any.
func AsProgramError(err error) (ProgramError, bool) {
	var pe ProgramError
	if errors.As(err, &pe) {
		return pe, true
	}
	return ProgramError{}, false
}
