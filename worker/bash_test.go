package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// eventCollector collects bash events for testing
type eventCollector struct {
	stdout   []string
	stderr   []string
	exitCode int32
	hasExit  bool
}

func (c *eventCollector) send(event *remotehandsv1.RunBashEvent) error {
	switch e := event.Event.(type) {
	case *remotehandsv1.RunBashEvent_Stdout:
		c.stdout = append(c.stdout, e.Stdout)
	case *remotehandsv1.RunBashEvent_Stderr:
		c.stderr = append(c.stderr, e.Stderr)
	case *remotehandsv1.RunBashEvent_ExitCode:
		c.exitCode = e.ExitCode
		c.hasExit = true
	}
	return nil
}

// =============================================================================
// Basic Execution Tests
// =============================================================================

func TestService_RunBash_EchoCommand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo hello world", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.True(t, collector.hasExit, "should have exit code event")
	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "hello world", collector.stdout[0])
	assert.Empty(t, collector.stderr)
}

func TestService_RunBash_MultilineOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo line1; echo line2; echo line3", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 3)
	assert.Equal(t, []string{"line1", "line2", "line3"}, collector.stdout)
}

func TestService_RunBash_ExitCode42(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "exit 42", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.True(t, collector.hasExit)
	assert.Equal(t, int32(42), collector.exitCode)
}

func TestService_RunBash_NonZeroExitWithOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo before; exit 5; echo after", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(5), collector.exitCode)
	// "after" should not appear because exit terminates the script
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "before", collector.stdout[0])
}

// =============================================================================
// Stderr Tests
// =============================================================================

func TestService_RunBash_StderrCapturedSeparately(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo stdout; echo stderr >&2", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "stdout", collector.stdout[0])
	require.Len(t, collector.stderr, 1)
	assert.Equal(t, "stderr", collector.stderr[0])
}

func TestService_RunBash_MixedStdoutStderr(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo out1; echo err1 >&2; echo out2; echo err2 >&2", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	assert.Equal(t, []string{"out1", "out2"}, collector.stdout)
	assert.Equal(t, []string{"err1", "err2"}, collector.stderr)
}

func TestService_RunBash_OnlyStderr(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo error message >&2; exit 1", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(1), collector.exitCode)
	assert.Empty(t, collector.stdout)
	require.Len(t, collector.stderr, 1)
	assert.Equal(t, "error message", collector.stderr[0])
}

// =============================================================================
// Timeout Tests
// =============================================================================

func TestService_RunBash_TimeoutKillsProcess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}

	start := time.Now()
	err := svc.runBash(ctx, "sleep 60", 100, nil, "", collector.send) // 100ms timeout
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.True(t, collector.hasExit)
	assert.Equal(t, int32(ExitCodeTimeout), collector.exitCode)

	// Should complete much faster than 60 seconds
	assert.Less(t, elapsed, 5*time.Second)
}

func TestService_RunBash_TimeoutWithPartialOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	// Echo something, then sleep
	err := svc.runBash(ctx, "echo before timeout; sleep 60", 200, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(ExitCodeTimeout), collector.exitCode)
	// Should have captured the output before timeout
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "before timeout", collector.stdout[0])
}

func TestService_RunBash_DefaultTimeout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	// Quick command should complete well within default 30s timeout
	err := svc.runBash(ctx, "echo quick", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	assert.Equal(t, []string{"quick"}, collector.stdout)
}

// =============================================================================
// Working Directory Tests
// =============================================================================

func TestService_RunBash_WorkingDirectoryRespected(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create a subdirectory with a file
	subdir := filepath.Join(homeDir, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(subdir, "marker.txt"), []byte("found"), 0644))

	collector := &eventCollector{}
	err := svc.runBash(ctx, "cat marker.txt", 0, nil, "subdir", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "found", collector.stdout[0])
}

func TestService_RunBash_WorkingDirectoryPwd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	subdir := filepath.Join(homeDir, "myworkdir")
	require.NoError(t, os.MkdirAll(subdir, 0755))

	collector := &eventCollector{}
	err := svc.runBash(ctx, "pwd", 0, nil, "myworkdir", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	// pwd output should contain the subdir name
	assert.True(t, strings.HasSuffix(collector.stdout[0], "myworkdir"), "pwd should end with myworkdir, got: %s", collector.stdout[0])
}

func TestService_RunBash_DefaultWorkingDirectoryIsHome(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	// Create a file in home directory
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "inroot.txt"), []byte("root"), 0644))

	collector := &eventCollector{}
	err := svc.runBash(ctx, "cat inroot.txt", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	assert.Equal(t, []string{"root"}, collector.stdout)
}

func TestService_RunBash_WorkingDirectoryPathTraversal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "pwd", 0, nil, "../../../tmp", collector.send)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

// =============================================================================
// Environment Variable Tests
// =============================================================================

func TestService_RunBash_EnvironmentVariablesSet(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	env := map[string]string{
		"MY_VAR":      "hello",
		"ANOTHER_VAR": "world",
	}
	err := svc.runBash(ctx, "echo $MY_VAR $ANOTHER_VAR", 0, env, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "hello world", collector.stdout[0])
}

func TestService_RunBash_EnvironmentVariableOverwrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	env := map[string]string{
		"PATH": "/custom/path",
	}
	err := svc.runBash(ctx, "echo $PATH", 0, env, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	// The PATH should include /custom/path (appended to existing)
	assert.Contains(t, collector.stdout[0], "/custom/path")
}

func TestService_RunBash_EmptyEnvironment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo hello", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	assert.Equal(t, []string{"hello"}, collector.stdout)
}

// =============================================================================
// Error Cases
// =============================================================================

func TestService_RunBash_EmptyCommandError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "", 0, nil, "", collector.send)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "command is required")
}

func TestService_RunBash_CommandNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "nonexistent_command_xyz_123", 0, nil, "", collector.send)
	require.NoError(t, err)

	// Command not found should result in non-zero exit (typically 127)
	assert.NotEqual(t, int32(0), collector.exitCode)
	// Stderr should contain error message
	assert.NotEmpty(t, collector.stderr)
}

func TestService_RunBash_SyntaxError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "if then else", 0, nil, "", collector.send)
	require.NoError(t, err)

	// Syntax error should result in non-zero exit
	assert.NotEqual(t, int32(0), collector.exitCode)
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestService_RunBash_NoOutput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "true", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.True(t, collector.hasExit)
	assert.Equal(t, int32(0), collector.exitCode)
	assert.Empty(t, collector.stdout)
	assert.Empty(t, collector.stderr)
}

func TestService_RunBash_LongLines(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	// Generate a long line (10KB)
	longLine := strings.Repeat("x", 10000)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo '"+longLine+"'", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, longLine, collector.stdout[0])
}

func TestService_RunBash_SpecialCharacters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, `echo "hello 'world' \"quoted\" $PATH"`, 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	// The output should contain the literal text (with $PATH expanded)
	assert.Contains(t, collector.stdout[0], "hello")
	assert.Contains(t, collector.stdout[0], "world")
}

func TestService_RunBash_FileOperations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, homeDir := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo content > test.txt && cat test.txt && rm test.txt", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "content", collector.stdout[0])

	// Verify file was deleted
	_, err = os.Stat(filepath.Join(homeDir, "test.txt"))
	assert.True(t, os.IsNotExist(err))
}

func TestService_RunBash_PipeCommands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo -e 'line1\nline2\nline3' | grep line2", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "line2", collector.stdout[0])
}

// =============================================================================
// Context Cancellation Tests
// =============================================================================

func TestService_RunBash_ContextCancelledBeforeStart(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo hello", 0, nil, "", collector.send)

	// The command will start but be immediately killed due to cancelled context
	// This results in a timeout exit code, not an error
	require.NoError(t, err)
	assert.True(t, collector.hasExit)
	assert.Equal(t, int32(ExitCodeTimeout), collector.exitCode)
}

// =============================================================================
// CmdCustomizer Tests
// =============================================================================

func TestService_RunBash_CmdCustomizerSetsEnv(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	// Use a customizer that replaces the environment entirely.
	svc.SetCmdCustomizer(func(cmd *exec.Cmd) {
		cmd.Env = []string{"SANDBOX_VAR=sandboxed", "PATH=/usr/bin:/bin"}
	})

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo $SANDBOX_VAR", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "sandboxed", collector.stdout[0])
}

func TestService_RunBash_CmdCustomizerClearsInheritedEnv(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	// Simulate the overwatch-agent pattern: clear env so parent vars are gone.
	svc.SetCmdCustomizer(func(cmd *exec.Cmd) {
		cmd.Env = []string{"HOME=/tmp", "PATH=/usr/bin:/bin"}
	})

	collector := &eventCollector{}
	// HOME should be /tmp, not the real home
	err := svc.runBash(ctx, "echo $HOME", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "/tmp", collector.stdout[0])
}

func TestService_RunBash_CmdCustomizerOverridesRequestEnv(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	// Customizer replaces entire env — request-level env vars set before
	// the customizer should be gone.
	svc.SetCmdCustomizer(func(cmd *exec.Cmd) {
		cmd.Env = []string{"PATH=/usr/bin:/bin"}
	})

	collector := &eventCollector{}
	env := map[string]string{"MY_VAR": "should_be_gone"}
	err := svc.runBash(ctx, "echo ${MY_VAR:-empty}", 0, env, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	require.Len(t, collector.stdout, 1)
	assert.Equal(t, "empty", collector.stdout[0])
}

func TestService_RunBash_CmdCustomizerPreservesSetpgid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	// Customizer that sets SysProcAttr without Setpgid — Setpgid should
	// still be applied by runBash after the customizer.
	var capturedSysProcAttr *syscall.SysProcAttr
	svc.SetCmdCustomizer(func(cmd *exec.Cmd) {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		capturedSysProcAttr = cmd.SysProcAttr
	})

	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo test", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	// Verify Setpgid was re-applied after customizer
	require.NotNil(t, capturedSysProcAttr)
	assert.True(t, capturedSysProcAttr.Setpgid, "Setpgid should be true after customizer")
}

func TestService_RunBash_NilCmdCustomizerIsNoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newTestService(t)

	// No customizer set — should work exactly as before.
	collector := &eventCollector{}
	err := svc.runBash(ctx, "echo works", 0, nil, "", collector.send)
	require.NoError(t, err)

	assert.Equal(t, int32(0), collector.exitCode)
	assert.Equal(t, []string{"works"}, collector.stdout)
}
