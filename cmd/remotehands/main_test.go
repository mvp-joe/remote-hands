package main

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain_MissingHome(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "--listen", "127.0.0.1:0")
	output, err := cmd.CombinedOutput()

	require.Error(t, err)
	assert.Contains(t, string(output), "--home is required")
}

func TestMain_BothListenAndSocket(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "run", ".", "--listen", "127.0.0.1:0", "--socket", "/tmp/test.sock", "--home", t.TempDir())
	output, err := cmd.CombinedOutput()

	require.Error(t, err)
	assert.Contains(t, string(output), "mutually exclusive")
}
