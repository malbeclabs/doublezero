package handlers_test

import (
	"errors"
	"testing"

	"github.com/malbeclabs/doublezero/lake/api/handlers"
	"github.com/stretchr/testify/assert"
)

func TestSanitizeError_NilError(t *testing.T) {
	result := handlers.SanitizeError(nil)
	assert.Equal(t, "", result)
}

func TestSanitizeError_PlainError(t *testing.T) {
	err := errors.New("something went wrong")
	result := handlers.SanitizeError(err)
	assert.Equal(t, "something went wrong", result)
}

func TestSanitizeError_RemovesCredentialsFromURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "URL with user:pass",
			input:    "failed to connect: postgres://user:secretpass@localhost:5432/db",
			expected: "failed to connect: postgres://***@localhost:5432/db",
		},
		{
			name:     "URL with just user",
			input:    "error at: postgres://admin@localhost:5432/db",
			expected: "error at: postgres://***@localhost:5432/db",
		},
		{
			name:     "HTTPS URL with credentials",
			input:    "cannot reach: https://api_key:secret123@api.example.com/v1",
			expected: "cannot reach: https://***@api.example.com/v1",
		},
		{
			name:     "URL without credentials",
			input:    "connecting to: postgres://localhost:5432/db",
			expected: "connecting to: postgres://localhost:5432/db",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.input)
			result := handlers.SanitizeError(err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeError_RemovesQueryParameters(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "URL with query params",
			input:    "error fetching: https://api.example.com/data?token=secret123&foo=bar",
			expected: "error fetching: https://api.example.com/data?...",
		},
		{
			name:     "URL with query ending in space",
			input:    "GET https://api.example.com?key=secret failed",
			expected: "GET https://api.example.com?... failed",
		},
		{
			name:     "URL with query in quotes",
			input:    "requesting 'https://api.example.com?pass=xxx' returned error",
			expected: "requesting 'https://api.example.com?...' returned error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.input)
			result := handlers.SanitizeError(err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeError_CombinedCredentialsAndQuery(t *testing.T) {
	err := errors.New("connect to: postgres://user:pass@localhost:5432/db?sslmode=disable")
	result := handlers.SanitizeError(err)

	// Should remove credentials first, then query params
	assert.Contains(t, result, "***@localhost")
	assert.Contains(t, result, "?...")
	assert.NotContains(t, result, "user:pass")
	assert.NotContains(t, result, "sslmode")
}

func TestSanitizeError_NoProtocol(t *testing.T) {
	// Error without :// should not be modified for credentials
	err := errors.New("failed: user@host denied")
	result := handlers.SanitizeError(err)
	// The @ without :// protocol shouldn't trigger credential removal
	assert.Equal(t, "failed: user@host denied", result)
}

func TestSanitizeError_MultipleURLs(t *testing.T) {
	// Only the first URL is processed
	err := errors.New("from https://user:pass@a.com to https://user2:pass2@b.com")
	result := handlers.SanitizeError(err)

	// First URL should have credentials removed
	assert.Contains(t, result, "https://***@a.com")
}
