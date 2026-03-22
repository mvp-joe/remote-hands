package worker

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"connectrpc.com/connect"
)

// gitCommit stages files and creates a commit.
// If files is empty, commits all currently staged changes.
// If files is specified, stages those files first then commits.
// Returns the commit SHA.
func (s *Service) gitCommit(ctx context.Context, repoPath, message string, files []string) (string, error) {
	if message == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("commit message is required"))
	}

	absRepoPath, err := ValidatePath(s.homeDir, repoPath)
	if err == ErrPathTraversal {
		return "", connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("path validation failed: %w", err))
	}

	// Ensure git user is configured (required for commit)
	if err := s.ensureGitConfig(ctx, absRepoPath); err != nil {
		return "", err
	}

	// Stage files if specified
	if len(files) > 0 {
		for _, file := range files {
			// Validate each file path
			_, err := ValidatePath(s.homeDir, file)
			if err == ErrPathTraversal {
				return "", connect.NewError(connect.CodePermissionDenied,
					fmt.Errorf("file path traversal: %s", file))
			}

			addCmd := exec.CommandContext(ctx, "git", "add", file)
			addCmd.Dir = absRepoPath

			var stderr bytes.Buffer
			addCmd.Stderr = &stderr

			if err := addCmd.Run(); err != nil {
				return "", connect.NewError(connect.CodeInternal,
					fmt.Errorf("git add %s failed: %s", file, stderr.String()))
			}
		}
	}

	// Create the commit
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", message)
	commitCmd.Dir = absRepoPath

	var stdout, stderr bytes.Buffer
	commitCmd.Stdout = &stdout
	commitCmd.Stderr = &stderr

	if err := commitCmd.Run(); err != nil {
		stderrStr := stderr.String()
		stderrLower := strings.ToLower(stderrStr)
		stdoutLower := strings.ToLower(stdout.String())
		if strings.Contains(stderrLower, "not a git repository") {
			return "", connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("not a git repository"))
		}
		if strings.Contains(stderrLower, "nothing to commit") || strings.Contains(stdoutLower, "nothing to commit") {
			return "", connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("nothing to commit"))
		}
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("git commit failed: %s", stderrStr))
	}

	// Extract commit SHA from output or run git rev-parse HEAD
	sha, err := s.getHeadCommitSHA(ctx, absRepoPath)
	if err != nil {
		return "", err
	}

	return sha, nil
}

// ensureGitConfig sets user.email and user.name if not already configured.
// This is required for git commit to work.
func (s *Service) ensureGitConfig(ctx context.Context, repoPath string) error {
	// Check if user.email is set
	checkEmail := exec.CommandContext(ctx, "git", "config", "user.email")
	checkEmail.Dir = repoPath
	if err := checkEmail.Run(); err != nil {
		// Not set, configure it
		setEmail := exec.CommandContext(ctx, "git", "config", "user.email", "remotehands@local")
		setEmail.Dir = repoPath
		var stderr bytes.Buffer
		setEmail.Stderr = &stderr
		if err := setEmail.Run(); err != nil {
			return connect.NewError(connect.CodeInternal,
				fmt.Errorf("failed to set git user.email: %s", stderr.String()))
		}
	}

	// Check if user.name is set
	checkName := exec.CommandContext(ctx, "git", "config", "user.name")
	checkName.Dir = repoPath
	if err := checkName.Run(); err != nil {
		// Not set, configure it
		setName := exec.CommandContext(ctx, "git", "config", "user.name", "Remote Hands")
		setName.Dir = repoPath
		var stderr bytes.Buffer
		setName.Stderr = &stderr
		if err := setName.Run(); err != nil {
			return connect.NewError(connect.CodeInternal,
				fmt.Errorf("failed to set git user.name: %s", stderr.String()))
		}
	}

	return nil
}

// getHeadCommitSHA returns the SHA of the HEAD commit.
func (s *Service) getHeadCommitSHA(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repoPath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", connect.NewError(connect.CodeInternal,
			fmt.Errorf("failed to get commit SHA: %s", stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}
