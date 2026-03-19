package worker

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
)

// stopGracePeriod is the time to wait after SIGTERM before sending SIGKILL.
const stopGracePeriod = 5 * time.Second

// tailEvent wraps a single line of output for fan-out to Tail subscribers.
type tailEvent struct {
	stdout bool   // true = stdout, false = stderr
	line   string // the line content (without trailing newline)
}

// trackedProcess holds all state for a managed process.
type trackedProcess struct {
	pid        int32
	name       string
	command    string
	workingDir string
	status     string     // "running" | "exited"
	exitCode   *int32     // nil while running
	startedAt  time.Time
	exitedAt   *time.Time

	cmd *exec.Cmd

	// subscribers receives live output lines. Protected by ProcessManager.mu.
	subscribers []chan tailEvent

	// done is closed when the process exits (wait goroutine completes).
	done chan struct{}
}

// ProcessInfo holds metadata about a managed process.
// This is the internal representation; the service layer maps to proto types.
type ProcessInfo struct {
	PID        int32
	Name       string
	Command    string
	WorkingDir string
	Status     string // "running" | "exited"
	ExitCode   *int32
	StartedAt  time.Time
	ExitedAt   *time.Time
}

// ProcessManager manages long-running processes within the working directory.
// All public methods are safe for concurrent use.
type ProcessManager struct {
	homeDir string
	logDir  string
	selfPID int32
	logger  *slog.Logger

	mu        sync.Mutex
	processes map[int32]*trackedProcess
}

// NewProcessManager creates a ProcessManager and ensures the log directory exists.
func NewProcessManager(homeDir string, logger *slog.Logger) (*ProcessManager, error) {
	logDir := filepath.Join(homeDir, ".process-logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("create process log directory: %w", err)
	}

	return &ProcessManager{
		homeDir:   homeDir,
		logDir:    logDir,
		selfPID:   int32(os.Getpid()),
		logger:    logger,
		processes: make(map[int32]*trackedProcess),
	}, nil
}

// Start launches a background process via bash -c. Stdout/stderr are tee'd to
// both disk log files and any active Tail subscribers. Returns the OS PID.
func (pm *ProcessManager) Start(
	ctx context.Context,
	command, name string,
	env map[string]string,
	workingDir string,
) (int32, error) {
	if command == "" {
		return 0, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("command is required"))
	}

	// Validate and resolve working directory.
	absWorkDir, err := ValidatePath(pm.homeDir, workingDir)
	if err == ErrPathTraversal {
		return 0, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("working directory path traversal: %w", err))
	}
	if err != nil {
		return 0, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to validate working directory: %w", err))
	}

	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = absWorkDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Merge env vars on top of the current environment.
	if len(env) > 0 {
		cmd.Env = cmd.Environ()
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return 0, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create stdout pipe: %w", err))
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return 0, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create stderr pipe: %w", err))
	}

	if err := cmd.Start(); err != nil {
		return 0, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to start command: %w", err))
	}

	pid := int32(cmd.Process.Pid)

	// Create log files.
	stdoutLog, err := os.Create(filepath.Join(pm.logDir, fmt.Sprintf("%d.stdout", pid)))
	if err != nil {
		// Best effort: kill the process we just started.
		_ = syscall.Kill(-int(pid), syscall.SIGKILL)
		return 0, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create stdout log file: %w", err))
	}

	stderrLog, err := os.Create(filepath.Join(pm.logDir, fmt.Sprintf("%d.stderr", pid)))
	if err != nil {
		stdoutLog.Close()
		_ = syscall.Kill(-int(pid), syscall.SIGKILL)
		return 0, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create stderr log file: %w", err))
	}

	proc := &trackedProcess{
		pid:        pid,
		name:       name,
		command:    command,
		workingDir: absWorkDir,
		status:     "running",
		startedAt:  time.Now(),
		cmd:        cmd,
		done:       make(chan struct{}),
	}

	pm.mu.Lock()
	pm.processes[pid] = proc
	pm.mu.Unlock()

	// Start pipe reader goroutines that tee output to disk and subscribers.
	var pipeWg sync.WaitGroup
	pipeWg.Add(2)
	go pm.pipeReader(proc, stdoutPipe, stdoutLog, true, &pipeWg)
	go pm.pipeReader(proc, stderrPipe, stderrLog, false, &pipeWg)

	// Background wait goroutine: waits for pipes to drain, then cmd.Wait().
	go pm.waitForExit(proc, &pipeWg)

	pm.logger.Info("process started", "pid", pid, "name", name, "command", command)
	return pid, nil
}

// pipeReader reads lines from a pipe, writes them to the log file, and fans
// out to all active Tail subscriber channels. It runs in its own goroutine.
func (pm *ProcessManager) pipeReader(
	proc *trackedProcess,
	pipe io.ReadCloser,
	logFile *os.File,
	isStdout bool,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	defer logFile.Close()

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()

		// Write to disk log file.
		fmt.Fprintln(logFile, line)

		// Fan out to subscribers.
		evt := tailEvent{stdout: isStdout, line: line}
		pm.mu.Lock()
		for _, ch := range proc.subscribers {
			select {
			case ch <- evt:
			default:
				// Subscriber is slow; drop the line to avoid blocking the pipe.
			}
		}
		pm.mu.Unlock()
	}

	if err := scanner.Err(); err != nil {
		pm.logger.Warn("pipe reader error", "pid", proc.pid, "stdout", isStdout, "error", err)
	}
}

// waitForExit waits for the pipe readers to finish draining, then calls
// cmd.Wait() to capture the exit code. It updates the tracked process status
// and closes all subscriber channels.
func (pm *ProcessManager) waitForExit(proc *trackedProcess, pipeWg *sync.WaitGroup) {
	// Wait for pipe readers to drain before calling cmd.Wait().
	pipeWg.Wait()

	waitErr := proc.cmd.Wait()

	var exitCode int32
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			exitCode = int32(exitErr.ExitCode())
		} else {
			pm.logger.Warn("unexpected wait error", "pid", proc.pid, "error", waitErr)
			exitCode = 1
		}
	}

	now := time.Now()

	pm.mu.Lock()
	proc.status = "exited"
	proc.exitCode = &exitCode
	proc.exitedAt = &now

	// Close all subscriber channels to signal Tail consumers.
	for _, ch := range proc.subscribers {
		close(ch)
	}
	proc.subscribers = nil
	pm.mu.Unlock()

	close(proc.done)
	pm.logger.Info("process exited", "pid", proc.pid, "exit_code", exitCode)
}

// Stop sends SIGTERM to a process group, waits up to the grace period, then
// sends SIGKILL if the process is still alive.
func (pm *ProcessManager) Stop(pid int32) error {
	pm.mu.Lock()
	proc, ok := pm.processes[pid]
	if !ok {
		pm.mu.Unlock()
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("process %d not found", pid))
	}
	if proc.status == "exited" {
		pm.mu.Unlock()
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("process %d has already exited", pid))
	}
	pm.mu.Unlock()

	// Send SIGTERM to the entire process group.
	if err := syscall.Kill(-int(pid), syscall.SIGTERM); err != nil {
		// Process may have already exited between our check and the kill.
		// If so, that's fine.
		if !errors.Is(err, syscall.ESRCH) {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to send SIGTERM: %w", err))
		}
	}

	// Wait for process to exit within the grace period.
	select {
	case <-proc.done:
		return nil
	case <-time.After(stopGracePeriod):
	}

	// Process didn't exit in time; send SIGKILL.
	if err := syscall.Kill(-int(pid), syscall.SIGKILL); err != nil {
		if !errors.Is(err, syscall.ESRCH) {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to send SIGKILL: %w", err))
		}
	}

	// Wait for the process to fully exit after SIGKILL.
	<-proc.done
	return nil
}

// List returns info for tracked processes, excluding the server's own PID.
// If includeExited is false, only running processes are returned.
func (pm *ProcessManager) List(includeExited bool) []*ProcessInfo {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	var result []*ProcessInfo
	for _, proc := range pm.processes {
		if proc.pid == pm.selfPID {
			continue
		}
		if !includeExited && proc.status != "running" {
			continue
		}
		result = append(result, &ProcessInfo{
			PID:        proc.pid,
			Name:       proc.name,
			Command:    proc.command,
			WorkingDir: proc.workingDir,
			Status:     proc.status,
			ExitCode:   proc.exitCode,
			StartedAt:  proc.startedAt,
			ExitedAt:   proc.exitedAt,
		})
	}
	return result
}

// StopAll stops all running processes in parallel and waits for completion.
func (pm *ProcessManager) StopAll() {
	pm.mu.Lock()
	var pids []int32
	for pid, proc := range pm.processes {
		if proc.status == "running" {
			pids = append(pids, pid)
		}
	}
	pm.mu.Unlock()

	var wg sync.WaitGroup
	for _, pid := range pids {
		wg.Add(1)
		go func(p int32) {
			defer wg.Done()
			if err := pm.Stop(p); err != nil {
				pm.logger.Warn("failed to stop process during StopAll", "pid", p, "error", err)
			}
		}(pid)
	}
	wg.Wait()
}

// Logs reads buffered stdout/stderr from disk for a given PID.
// head and tail are mutually exclusive line-limiting options.
func (pm *ProcessManager) Logs(pid, head, tail int32) (string, string, error) {
	pm.mu.Lock()
	_, ok := pm.processes[pid]
	pm.mu.Unlock()

	if !ok {
		return "", "", connect.NewError(connect.CodeNotFound, fmt.Errorf("process %d not found", pid))
	}

	if head > 0 && tail > 0 {
		return "", "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("head and tail are mutually exclusive"))
	}

	stdoutContent, err := readLogFile(filepath.Join(pm.logDir, fmt.Sprintf("%d.stdout", pid)), head, tail)
	if err != nil {
		return "", "", connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read stdout log: %w", err))
	}

	stderrContent, err := readLogFile(filepath.Join(pm.logDir, fmt.Sprintf("%d.stderr", pid)), head, tail)
	if err != nil {
		return "", "", connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read stderr log: %w", err))
	}

	return stdoutContent, stderrContent, nil
}

// readLogFile reads a log file and applies head/tail line limiting.
func readLogFile(path string, head, tail int32) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	content := string(data)
	if content == "" {
		return "", nil
	}

	if head <= 0 && tail <= 0 {
		return content, nil
	}

	// Split into lines, preserving the trailing newline behavior.
	// TrimRight removes the trailing newline so we don't get an extra empty
	// element from Split.
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")

	if head > 0 {
		if int(head) < len(lines) {
			lines = lines[:head]
		}
	} else if tail > 0 {
		if int(tail) < len(lines) {
			lines = lines[len(lines)-int(tail):]
		}
	}

	return strings.Join(lines, "\n") + "\n", nil
}

// Tail subscribes to live stdout/stderr from a running process. It blocks
// until the process exits (sending an exit_code event) or ctx is cancelled.
// Returns FailedPrecondition if the process has already exited.
func (pm *ProcessManager) Tail(
	ctx context.Context,
	pid int32,
	send func(*remotehandsv1.ProcessTailEvent) error,
) error {
	pm.mu.Lock()
	proc, ok := pm.processes[pid]
	if !ok {
		pm.mu.Unlock()
		return connect.NewError(connect.CodeNotFound, fmt.Errorf("process %d not found", pid))
	}
	if proc.status == "exited" {
		pm.mu.Unlock()
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("process %d has already exited, use ProcessLogs instead", pid))
	}

	// Register a subscriber channel. Buffer it so the pipe reader doesn't
	// block if we're slightly behind.
	ch := make(chan tailEvent, 256)
	proc.subscribers = append(proc.subscribers, ch)
	pm.mu.Unlock()

	// Ensure we deregister on exit.
	defer pm.removeSubscriber(pid, ch)

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-ch:
			if !ok {
				// Channel closed: process exited. Send exit_code event.
				pm.mu.Lock()
				exitCode := int32(0)
				if proc.exitCode != nil {
					exitCode = *proc.exitCode
				}
				pm.mu.Unlock()

				_ = send(&remotehandsv1.ProcessTailEvent{
					Event: &remotehandsv1.ProcessTailEvent_ExitCode{ExitCode: exitCode},
				})
				return nil
			}

			var protoEvt *remotehandsv1.ProcessTailEvent
			if evt.stdout {
				protoEvt = &remotehandsv1.ProcessTailEvent{
					Event: &remotehandsv1.ProcessTailEvent_Stdout{Stdout: evt.line},
				}
			} else {
				protoEvt = &remotehandsv1.ProcessTailEvent{
					Event: &remotehandsv1.ProcessTailEvent_Stderr{Stderr: evt.line},
				}
			}

			if err := send(protoEvt); err != nil {
				return fmt.Errorf("failed to send tail event: %w", err)
			}
		}
	}
}

// removeSubscriber removes a channel from the process's subscriber list.
func (pm *ProcessManager) removeSubscriber(pid int32, ch chan tailEvent) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	proc, ok := pm.processes[pid]
	if !ok {
		return
	}

	for i, sub := range proc.subscribers {
		if sub == ch {
			proc.subscribers = append(proc.subscribers[:i], proc.subscribers[i+1:]...)
			return
		}
	}
}
