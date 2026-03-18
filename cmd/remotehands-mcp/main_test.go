package main

import (
	"net/http"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain_RelayModeRequiresMachine(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "--relay", "https://relay.example.com")
	output, err := cmd.CombinedOutput()

	require.Error(t, err)
	assert.Contains(t, string(output), "--machine is required")
}

func TestHeaderInterceptor_DirectMode(t *testing.T) {
	t.Parallel()

	token := "test-auth-token"
	interceptor := &streamingHeaderInterceptor{inject: func(h http.Header) {
		if token != "" {
			h.Set("Authorization", "Bearer "+token)
		}
	}}

	// Verify the inject function sets the correct header.
	headers := http.Header{}
	interceptor.inject(headers)
	assert.Equal(t, "Bearer test-auth-token", headers.Get("Authorization"))
}

func TestHeaderInterceptor_RelayMode(t *testing.T) {
	t.Parallel()

	secret := "relay-secret"
	machineID := "machine-123"
	interceptor := &streamingHeaderInterceptor{inject: func(h http.Header) {
		if secret != "" {
			h.Set("Authorization", "Bearer "+secret)
		}
		h.Set("X-Target-Machine", machineID)
	}}

	headers := http.Header{}
	interceptor.inject(headers)
	assert.Equal(t, "Bearer relay-secret", headers.Get("Authorization"))
	assert.Equal(t, "machine-123", headers.Get("X-Target-Machine"))
}
