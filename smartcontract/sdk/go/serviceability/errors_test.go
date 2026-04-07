package serviceability

import (
	"encoding/json"
	"errors"
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
