package serviceability

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
)

// ProgramError maps a custom program error code to a human-readable name and description.
type ProgramError struct {
	Name        string
	Description string
}

// programErrors maps custom error codes from the doublezero-serviceability program
// (DoubleZeroError enum) to their names and descriptions.
var programErrors = map[uint32]ProgramError{
	1:  {Name: "InvalidOwnerPubkey", Description: "Only the owner can perform this action"},
	2:  {Name: "InvalidLocationPubkey", Description: "You are trying to assign a Pubkey that does not correspond to a Location"},
	3:  {Name: "InvalidExchangePubkey", Description: "You are trying to assign a Pubkey that does not correspond to a Exchange"},
	4:  {Name: "InvalidDeviceAPubkey", Description: "You are trying to assign a Pubkey that does not correspond to a Device A"},
	5:  {Name: "InvalidDeviceZPubkey", Description: "You are trying to assign a Pubkey that does not correspond to a Device Z"},
	6:  {Name: "InvalidDevicePubkey", Description: "You are trying to assign a Pubkey that does not correspond to a Device"},
	7:  {Name: "InvalidStatus", Description: "Invalid Status"},
	8:  {Name: "NotAllowed", Description: "You are not allowed to execute this action"},
	9:  {Name: "InvalidAccountType", Description: "Invalid Account Type"},
	10: {Name: "InvalidContributorPubkey", Description: "You are trying to assign a Pubkey that does not correspond to a Contributor"},
	11: {Name: "InvalidInterfaceVersion", Description: "Invalid Interface Version"},
	12: {Name: "InvalidInterfaceName", Description: "Invalid Interface Name"},
	13: {Name: "ReferenceCountNotZero", Description: "Reference Count is not zero"},
	14: {Name: "InvalidContributor", Description: "Invalid Contributor"},
	15: {Name: "InvalidInterfaceZForExternal", Description: "Invalid External Link: Side Z interface name should be empty"},
	16: {Name: "InvalidIndex", Description: "Invalid index"},
	17: {Name: "DeviceAlreadySet", Description: "Device already set"},
	18: {Name: "DeviceNotSet", Description: "Device not set"},
	19: {Name: "InvalidAccountCode", Description: "Invalid account code"},
	20: {Name: "MaxUsersExceeded", Description: "Max users exceeded"},
	21: {Name: "InvalidLastAccessEpoch", Description: "Invalid last access epoch"},
	22: {Name: "Unauthorized", Description: "Unauthorized"},
	23: {Name: "InvalidSolanaPubkey", Description: "Invalid Solana Validator Pubkey"},
	24: {Name: "InterfaceNotFound", Description: "InterfaceNotFound"},
	25: {Name: "AccessPassUnauthorized", Description: "Invalid Access Pass"},
	26: {Name: "InvalidClientIp", Description: "Invalid Client IP"},
	27: {Name: "InvalidDzIp", Description: "Invalid DZ IP"},
	28: {Name: "InvalidTunnelNet", Description: "Invalid Tunnel Network"},
	29: {Name: "InvalidTunnelId", Description: "Invalid Tunnel ID"},
	30: {Name: "InvalidTunnelIp", Description: "Invalid Tunnel IP"},
	31: {Name: "InvalidBandwidth", Description: "Invalid Bandwidth"},
	32: {Name: "InvalidDelay", Description: "Invalid Delay"},
	33: {Name: "InvalidJitter", Description: "Invalid Jitter"},
	34: {Name: "CodeTooLong", Description: "Code too long"},
	35: {Name: "NoDzPrefixes", Description: "No DZ Prefixes"},
	36: {Name: "InvalidLocation", Description: "Invalid Location"},
	37: {Name: "InvalidExchange", Description: "Invalid Exchange"},
	38: {Name: "InvalidDzPrefix", Description: "Invalid DZ Prefix"},
	39: {Name: "NameTooLong", Description: "Name too long"},
	40: {Name: "InvalidLatitude", Description: "Invalid Latitude"},
	41: {Name: "InvalidLongitude", Description: "Invalid Longitude"},
	42: {Name: "InvalidLocId", Description: "Invalid Location ID"},
	43: {Name: "InvalidCountryCode", Description: "Invalid Country Code"},
	44: {Name: "InvalidLocalAsn", Description: "Invalid Local ASN"},
	45: {Name: "InvalidRemoteAsn", Description: "Invalid Remote ASN"},
	46: {Name: "InvalidMtu", Description: "Invalid MTU"},
	47: {Name: "InvalidInterfaceIp", Description: "Invalid Interface IP"},
	48: {Name: "InvalidInterfaceIpNet", Description: "Invalid Interface IP Net"},
	49: {Name: "InvalidVlanId", Description: "Invalid VLAN ID"},
	50: {Name: "InvalidMaxBandwidth", Description: "Invalid Max Bandwidth"},
	51: {Name: "InvalidMulticastIp", Description: "Invalid Multicast IP"},
	52: {Name: "InvalidAccountOwner", Description: "Invalid Account Owner"},
	53: {Name: "AccessPassNotFound", Description: "Access Pass not found"},
	54: {Name: "UserAccountNotFound", Description: "User account not found"},
	55: {Name: "InvalidBgpCommunity", Description: "Invalid BGP Community"},
	56: {Name: "InterfaceAlreadyExists", Description: "Interface already exists"},
	57: {Name: "InvalidInterfaceType", Description: "Invalid Interface Type"},
	58: {Name: "InvalidLoopbackType", Description: "Invalid Loopback Type"},
	59: {Name: "InvalidMinCompatibleVersion", Description: "Invalid Minimum Compatible Version"},
	60: {Name: "InvalidActualLocation", Description: "Invalid Actual Location"},
	61: {Name: "InvalidUserPubkey", Description: "Invalid User Pubkey"},
	62: {Name: "InvalidPublicIp", Description: "Invalid Public IP: IP conflicts with DZ prefix"},
	63: {Name: "AllocationFailed", Description: "Allocation failed, resource exhausted"},
	64: {Name: "SerializationFailure", Description: "Serialization failed"},
	65: {Name: "InvalidArgument", Description: "Invalid argument"},
	66: {Name: "InvalidFoundationAllowlist", Description: "Invalid Foundation Allowlist: cannot be empty"},
	67: {Name: "Deprecated", Description: "Deprecated error"},
	68: {Name: "ImmutableField", Description: "Immutable Field"},
	69: {Name: "CyoaRequiresPhysical", Description: "CYOA can only be set on physical interfaces"},
	70: {Name: "DeviceHasInterfaces", Description: "Device can only be removed if it has no interfaces"},
	71: {Name: "MulticastGroupNotEmpty", Description: "MulticastGroup can only be deleted if it has no active publishers or subscribers"},
	72: {Name: "AccessPassInUse", Description: "Access Pass is in use (non-zero connection_count)"},
	73: {Name: "InvalidTenantPubkey", Description: "You are trying to assign a Pubkey that does not correspond to a Tenant"},
	74: {Name: "InvalidVrfId", Description: "Invalid VRF ID"},
	75: {Name: "VrfIdTooLong", Description: "VRF ID too long"},
	76: {Name: "AdministratorAlreadyExists", Description: "Administrator already exists"},
	77: {Name: "AdministratorNotFound", Description: "Administrator not found"},
	78: {Name: "InvalidPaymentStatus", Description: "Invalid Payment Status"},
	79: {Name: "TenantNotInAccessPassAllowlist", Description: "Tenant not in access-pass tenant_allowlist"},
	80: {Name: "InvalidTunnelEndpoint", Description: "Invalid Tunnel Endpoint"},
	81: {Name: "MaxUnicastUsersExceeded", Description: "Max unicast users exceeded"},
	82: {Name: "MaxMulticastSubscribersExceeded", Description: "Max multicast subscribers exceeded"},
	83: {Name: "InterfaceHasEdgeAssignment", Description: "Interface cannot have both a link and a CYOA or DIA assignment"},
	84: {Name: "FeatureNotEnabled", Description: "Feature not enabled"},
	85: {Name: "MaxMulticastPublishersExceeded", Description: "Max multicast publishers exceeded"},
	86: {Name: "ArithmeticOverflow", Description: "Arithmetic overflow"},
}

// ProgramErrorMessage returns a human-readable message for the given custom error code.
// For known codes it returns "ErrorName: description"; for unknown codes it returns
// "unknown error code N".
func ProgramErrorMessage(code uint32) string {
	if pe, ok := programErrors[code]; ok {
		return fmt.Sprintf("%s: %s", pe.Name, pe.Description)
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
