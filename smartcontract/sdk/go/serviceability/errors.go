package serviceability

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

// programErrors maps custom error codes from the doublezero-serviceability
// program (DoubleZeroError enum) to their names.
var programErrors = map[uint32]string{
	1:  "InvalidOwnerPubkey",
	2:  "InvalidLocationPubkey",
	3:  "InvalidExchangePubkey",
	4:  "InvalidDeviceAPubkey",
	5:  "InvalidDeviceZPubkey",
	6:  "InvalidDevicePubkey",
	7:  "InvalidStatus",
	8:  "NotAllowed",
	9:  "InvalidAccountType",
	10: "InvalidContributorPubkey",
	11: "InvalidInterfaceVersion",
	12: "InvalidInterfaceName",
	13: "ReferenceCountNotZero",
	14: "InvalidContributor",
	15: "InvalidInterfaceZForExternal",
	16: "InvalidIndex",
	17: "DeviceAlreadySet",
	18: "DeviceNotSet",
	19: "InvalidAccountCode",
	20: "MaxUsersExceeded",
	21: "InvalidLastAccessEpoch",
	22: "Unauthorized",
	23: "InvalidSolanaPubkey",
	24: "InterfaceNotFound",
	25: "AccessPassUnauthorized",
	26: "InvalidClientIp",
	27: "InvalidDzIp",
	28: "InvalidTunnelNet",
	29: "InvalidTunnelId",
	30: "InvalidTunnelIp",
	31: "InvalidBandwidth",
	32: "InvalidDelay",
	33: "InvalidJitter",
	34: "CodeTooLong",
	35: "NoDzPrefixes",
	36: "InvalidLocation",
	37: "InvalidExchange",
	38: "InvalidDzPrefix",
	39: "NameTooLong",
	40: "InvalidLatitude",
	41: "InvalidLongitude",
	42: "InvalidLocId",
	43: "InvalidCountryCode",
	44: "InvalidLocalAsn",
	45: "InvalidRemoteAsn",
	46: "InvalidMtu",
	47: "InvalidInterfaceIp",
	48: "InvalidInterfaceIpNet",
	49: "InvalidVlanId",
	50: "InvalidMaxBandwidth",
	51: "InvalidMulticastIp",
	52: "InvalidAccountOwner",
	53: "AccessPassNotFound",
	54: "UserAccountNotFound",
	55: "InvalidBgpCommunity",
	56: "InterfaceAlreadyExists",
	57: "InvalidInterfaceType",
	58: "InvalidLoopbackType",
	59: "InvalidMinCompatibleVersion",
	60: "InvalidActualLocation",
	61: "InvalidUserPubkey",
	62: "InvalidPublicIp",
	63: "AllocationFailed",
	64: "SerializationFailure",
	65: "InvalidArgument",
	66: "InvalidFoundationAllowlist",
	67: "Deprecated",
	68: "ImmutableField",
	69: "CyoaRequiresPhysical",
	70: "DeviceHasInterfaces",
	71: "MulticastGroupNotEmpty",
	72: "AccessPassInUse",
	73: "InvalidTenantPubkey",
	74: "InvalidVrfId",
	75: "VrfIdTooLong",
	76: "AdministratorAlreadyExists",
	77: "AdministratorNotFound",
	78: "InvalidPaymentStatus",
	79: "TenantNotInAccessPassAllowlist",
	80: "InvalidTunnelEndpoint",
	81: "MaxUnicastUsersExceeded",
	82: "MaxMulticastSubscribersExceeded",
	83: "InterfaceHasEdgeAssignment",
	84: "FeatureNotEnabled",
	85: "MaxMulticastPublishersExceeded",
	86: "ArithmeticOverflow",
}

// ProgramErrorMessage returns a human-readable message for the given custom error code.
func ProgramErrorMessage(code uint32) string {
	if name, ok := programErrors[code]; ok {
		return name
	}
	return fmt.Sprintf("unknown error code %d", code)
}

// parseProgramLogs extracts the "Program log: ..." lines from a jsonrpc.RPCError's
// Data["logs"] field. Returns nil if no logs are found.
func parseProgramLogs(err error) []string {
	var rpcErr *jsonrpc.RPCError
	if !errors.As(err, &rpcErr) {
		return nil
	}

	data, ok := rpcErr.Data.(map[string]any)
	if !ok {
		return nil
	}

	logsRaw, ok := data["logs"]
	if !ok {
		return nil
	}

	logsSlice, ok := logsRaw.([]any)
	if !ok {
		return nil
	}

	var logs []string
	for _, entry := range logsSlice {
		if s, ok := entry.(string); ok {
			logs = append(logs, s)
		}
	}
	return logs
}

// formatRPCError produces a concise, human-readable summary of a Solana RPC
// transaction error. It includes the program error name/description (if known)
// and any "Program log:" lines that are not just invoke/consumed/success noise.
func formatRPCError(err error) string {
	code, hasCode := parseCustomErrorCode(err)

	// Collect interesting program log lines (skip boilerplate).
	var interesting []string
	for _, line := range parseProgramLogs(err) {
		// Keep only "Program log:" lines that aren't instruction echo.
		if len(line) > 13 && line[:13] == "Program log: " {
			msg := line[13:]
			// Skip the "Instruction: ..." echo lines.
			if len(msg) > 13 && msg[:13] == "Instruction: " {
				continue
			}
			interesting = append(interesting, msg)
		}
	}

	switch {
	case hasCode && len(interesting) > 0:
		return fmt.Sprintf("%s (program logs: %v)", ProgramErrorMessage(code), interesting)
	case hasCode:
		return ProgramErrorMessage(code)
	case len(interesting) > 0:
		return fmt.Sprintf("transaction failed (program logs: %v)", interesting)
	default:
		return err.Error()
	}
}

// parseCustomErrorCode extracts the custom error code from a jsonrpc.RPCError.
// It looks at Data["err"]["InstructionError"][1]["Custom"] and handles both
// json.Number and float64 representations. Returns (code, true) on success
// or (0, false) on failure.
func parseCustomErrorCode(err error) (uint32, bool) {
	var rpcErr *jsonrpc.RPCError
	if !errors.As(err, &rpcErr) {
		return 0, false
	}

	data, ok := rpcErr.Data.(map[string]any)
	if !ok {
		return 0, false
	}

	errField, ok := data["err"]
	if !ok {
		return 0, false
	}

	errMap, ok := errField.(map[string]any)
	if !ok {
		return 0, false
	}

	instrErr, ok := errMap["InstructionError"]
	if !ok {
		return 0, false
	}

	instrErrSlice, ok := instrErr.([]any)
	if !ok || len(instrErrSlice) < 2 {
		return 0, false
	}

	customMap, ok := instrErrSlice[1].(map[string]any)
	if !ok {
		return 0, false
	}

	customVal, ok := customMap["Custom"]
	if !ok {
		return 0, false
	}

	switch v := customVal.(type) {
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return uint32(n), true
	case float64:
		return uint32(v), true
	default:
		return 0, false
	}
}
