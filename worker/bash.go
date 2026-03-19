package worker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
)

// DefaultBashTimeout is the default timeout for bash commands if not specified.
const DefaultBashTimeout = 30 * time.Second

// ExitCodeTimeout is a special exit code indicating the command timed out.
// This matches common shell conventions where 128+signal is used for signal termination.
const ExitCodeTimeout = 137 // 128 + SIGKILL(9)

// runBash executes a bash command and streams stdout/stderr events.
// The final event is always the exit code.
func (s *Service) runBash(
	ctx context.Context,
	command string,
	timeoutMs int32,
	env map[string]string,
	workingDir string,
	send func(*remotehandsv1.RunBashEvent) error,
) error {
	if command == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("command is required"))
	}

	// Determine timeout
	timeout := DefaultBashTimeout
	if timeoutMs > 0 {
		timeout = time.Duration(timeoutMs) * time.Millisecond
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Validate and resolve working directory
	absWorkDir, err := ValidatePath(s.homeDir, workingDir)
	if err == ErrPathTraversal {
		return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("working directory path traversal: %w", err))
	}
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to validate working directory: %w", err))
	}

	// Create the command - NOT using CommandContext because we need to kill
	// the entire process group on timeout, not just the main process
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = absWorkDir

	// Set environment variables before the customizer so the customizer
	// can override/replace the environment entirely if needed.
	if len(env) > 0 {
		// Start with current environment, then add custom vars
		cmd.Env = cmd.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	// Allow caller to customize the command (e.g. drop privileges, clear env).
	if s.cmdCustomizer != nil {
		s.cmdCustomizer(cmd)
	}

	// Always set up process group for proper cleanup - this allows us to kill
	// all child processes when the timeout is reached. Applied after the
	// customizer so Setpgid is always true regardless of what the customizer does.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	// Set up pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create stdout pipe: %w", err))
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create stderr pipe: %w", err))
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start command: %w", err))
	}

	// Track if we killed due to timeout
	var timedOut bool
	var timeoutMu sync.Mutex

	// Watch for context cancellation/timeout in a separate goroutine
	// and kill the process group when it happens
	go func() {
		<-ctx.Done()
		timeoutMu.Lock()
		timedOut = true
		timeoutMu.Unlock()

		if cmd.Process != nil {
			// Kill the entire process group (negative PID)
			// This ensures all child processes are also killed
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}()

	// Track errors from goroutines
	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Stream stdout
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if err := send(&remotehandsv1.RunBashEvent{
				Event: &remotehandsv1.RunBashEvent_Stdout{Stdout: line},
			}); err != nil {
				errCh <- fmt.Errorf("failed to send stdout: %w", err)
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("stdout scanner error: %w", err)
		}
	}()

	// Stream stderr
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			if err := send(&remotehandsv1.RunBashEvent{
				Event: &remotehandsv1.RunBashEvent_Stderr{Stderr: line},
			}); err != nil {
				errCh <- fmt.Errorf("failed to send stderr: %w", err)
				return
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- fmt.Errorf("stderr scanner error: %w", err)
		}
	}()

	// Wait for pipe readers to finish
	wg.Wait()
	close(errCh)

	// Check for pipe errors
	for pipeErr := range errCh {
		s.logger.Warn("pipe error during bash execution", "error", pipeErr)
	}

	// Wait for command to complete
	waitErr := cmd.Wait()

	// Determine exit code
	var exitCode int32
	timeoutMu.Lock()
	wasTimeout := timedOut
	timeoutMu.Unlock()

	if wasTimeout {
		exitCode = ExitCodeTimeout
	} else if waitErr != nil {
		// Extract exit code from error
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = int32(exitErr.ExitCode())
		} else {
			// Unexpected error - log and use non-zero exit
			s.logger.Warn("unexpected wait error", "error", waitErr)
			exitCode = 1
		}
	}

	// Send final exit code event
	if err := send(&remotehandsv1.RunBashEvent{
		Event: &remotehandsv1.RunBashEvent_ExitCode{ExitCode: exitCode},
	}); err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to send exit code: %w", err))
	}

	return nil
}
