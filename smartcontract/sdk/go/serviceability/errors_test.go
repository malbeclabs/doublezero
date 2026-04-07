package serviceability

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/gagliardetto/solana-go/rpc/jsonrpc"
	"github.com/stretchr/testify/assert"
)

func TestProgramErrorMessage(t *testing.T) {
	t.Run("known error code", func(t *testing.T) {
		msg := ProgramErrorMessage(8)
		assert.Equal(t, "NotAllowed: You are not allowed to execute this action", msg)
	})

	t.Run("unknown error code", func(t *testing.T) {
		msg := ProgramErrorMessage(9999)
		assert.Equal(t, "unknown error code 9999", msg)
	})

	t.Run("all codes have non-empty name and description", func(t *testing.T) {
		for code, pe := range programErrors {
			assert.NotEmpty(t, pe.Name, "code %d has empty Name", code)
			assert.NotEmpty(t, pe.Description, "code %d has empty Description", code)
		}
	})
}

func TestParseCustomErrorCode(t *testing.T) {
	t.Run("parses code from json.Number", func(t *testing.T) {
		rpcErr := &jsonrpc.RPCError{
			Code:    -32002,
			Message: "Transaction simulation failed",
			Data: map[string]any{
				"err": map[string]any{
					"InstructionError": []any{
						json.Number("0"),
						map[string]any{
							"Custom": json.Number("8"),
						},
					},
				},
			},
		}
		code, ok := parseCustomErrorCode(rpcErr)
		assert.True(t, ok)
		assert.Equal(t, uint32(8), code)
	})

	t.Run("parses code from float64", func(t *testing.T) {
		rpcErr := &jsonrpc.RPCError{
			Code:    -32002,
			Message: "Transaction simulation failed",
			Data: map[string]any{
				"err": map[string]any{
					"InstructionError": []any{
						float64(0),
						map[string]any{
							"Custom": float64(8),
						},
					},
				},
			},
		}
		code, ok := parseCustomErrorCode(rpcErr)
		assert.True(t, ok)
		assert.Equal(t, uint32(8), code)
	})

	t.Run("returns false for non-RPC error", func(t *testing.T) {
		code, ok := parseCustomErrorCode(errors.New("foo"))
		assert.False(t, ok)
		assert.Equal(t, uint32(0), code)
	})

	t.Run("returns false for missing err field", func(t *testing.T) {
		rpcErr := &jsonrpc.RPCError{
			Code:    -32002,
			Message: "Transaction simulation failed",
			Data:    map[string]any{},
		}
		code, ok := parseCustomErrorCode(rpcErr)
		assert.False(t, ok)
		assert.Equal(t, uint32(0), code)
	})

	t.Run("returns false for missing Custom field", func(t *testing.T) {
		rpcErr := &jsonrpc.RPCError{
			Code:    -32002,
			Message: "Transaction simulation failed",
			Data: map[string]any{
				"err": map[string]any{
					"InstructionError": []any{
						float64(0),
						"InvalidAccountData",
					},
				},
			},
		}
		code, ok := parseCustomErrorCode(rpcErr)
		assert.False(t, ok)
		assert.Equal(t, uint32(0), code)
	})
}

func TestParseProgramLogs(t *testing.T) {
	t.Run("extracts logs from RPC error", func(t *testing.T) {
		rpcErr := &jsonrpc.RPCError{
			Code:    -32002,
			Message: "Transaction simulation failed",
			Data: map[string]any{
				"logs": []any{
					"Program invoke [1]",
					"Program log: Instruction: SetDeviceHealth(health: ReadyForUsers)",
					"Program log: Invalid public IP: 0.1.2.3",
					"Program failed: custom program error: 0x1a",
				},
			},
		}
		logs := parseProgramLogs(rpcErr)
		assert.Equal(t, []string{
			"Program invoke [1]",
			"Program log: Instruction: SetDeviceHealth(health: ReadyForUsers)",
			"Program log: Invalid public IP: 0.1.2.3",
			"Program failed: custom program error: 0x1a",
		}, logs)
	})

	t.Run("returns nil for non-RPC error", func(t *testing.T) {
		assert.Nil(t, parseProgramLogs(errors.New("foo")))
	})

	t.Run("returns nil for missing logs", func(t *testing.T) {
		rpcErr := &jsonrpc.RPCError{Data: map[string]any{}}
		assert.Nil(t, parseProgramLogs(rpcErr))
	})
}

func TestFormatRPCError(t *testing.T) {
	t.Run("includes error name and program logs", func(t *testing.T) {
		rpcErr := &jsonrpc.RPCError{
			Code:    -32002,
			Message: "Transaction simulation failed",
			Data: map[string]any{
				"err": map[string]any{
					"InstructionError": []any{
						json.Number("1"),
						map[string]any{"Custom": json.Number("26")},
					},
				},
				"logs": []any{
					"Program invoke [1]",
					"Program log: Instruction: SetDeviceHealth(health: ReadyForUsers)",
					"Program log: Invalid public IP: 0.1.2.3",
					"Program failed: custom program error: 0x1a",
				},
			},
		}
		// Wrap it like executeTransaction does.
		wrapped := fmt.Errorf("failed to send transaction: %w", rpcErr)
		msg := formatRPCError(wrapped)
		assert.Equal(t, "InvalidClientIp: Invalid Client IP (program logs: [Invalid public IP: 0.1.2.3])", msg)
	})

	t.Run("known code without interesting logs", func(t *testing.T) {
		rpcErr := &jsonrpc.RPCError{
			Code:    -32002,
			Message: "Transaction simulation failed",
			Data: map[string]any{
				"err": map[string]any{
					"InstructionError": []any{
						json.Number("0"),
						map[string]any{"Custom": json.Number("8")},
					},
				},
				"logs": []any{
					"Program invoke [1]",
					"Program log: Instruction: SetDeviceHealth(health: ReadyForUsers)",
				},
			},
		}
		msg := formatRPCError(rpcErr)
		assert.Equal(t, "NotAllowed: You are not allowed to execute this action", msg)
	})

	t.Run("falls back to Error() for non-RPC errors", func(t *testing.T) {
		err := errors.New("some other error")
		assert.Equal(t, "some other error", formatRPCError(err))
	})
}
