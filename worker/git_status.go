package worker

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/go-git/go-git/v5"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
)

// gitStatus opens a git repository at repoPath and returns per-file status
// using go-git's Worktree.Status(). The mapping from go-git StatusCode to
// our simplified status strings preserves the same priority as the old
// exec.Command-based implementation:
//
//   - Staging field takes precedence over Worktree field
//   - Untracked requires both fields == Untracked
//   - Deleted if either field == Deleted
//   - Modified if either field == Modified; Renamed maps to "modified"
//   - Copied maps to "added"
func (s *Service) gitStatus(ctx context.Context, repoPath string) ([]*remotehandsv1.GitFileStatus, error) {
	absPath, err := ValidatePath(s.homeDir, repoPath)
	if err == ErrPathTraversal {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("path validation failed: %w", err))
	}

	repo, err := git.PlainOpen(absPath)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("not a git repository"))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("open repository: %w", err))
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get worktree: %w", err))
	}

	status, err := wt.Status()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("worktree status: %w", err))
	}

	var files []*remotehandsv1.GitFileStatus
	for path, fs := range status {
		mapped := mapGoGitStatus(fs.Staging, fs.Worktree)
		if mapped == "" {
			continue
		}
		files = append(files, &remotehandsv1.GitFileStatus{
			Path:   path,
			Status: mapped,
		})
	}

	return files, nil
}

// mapGoGitStatus converts go-git Staging/Worktree StatusCode pair to our
// simplified status string. The priority matches the original porcelain parser:
// staging field takes precedence; untracked requires both fields; deleted and
// modified checked in either field.
func mapGoGitStatus(staging, worktree git.StatusCode) string {
	// Untracked: both fields must be Untracked
	if staging == git.Untracked && worktree == git.Untracked {
		return "untracked"
	}

	// Staging takes precedence
	if staging == git.Added {
		return "added"
	}
	if staging == git.Copied {
		return "added"
	}

	// Deleted in either field
	if staging == git.Deleted || worktree == git.Deleted {
		return "deleted"
	}

	// Modified in either field
	if staging == git.Modified || worktree == git.Modified {
		return "modified"
	}

	// Renamed in staging (worktree rename not a thing in go-git)
	if staging == git.Renamed {
		return "modified"
	}

	return ""
}
