package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// resolveAuthor determines the commit author (name, email) using a four-level
// resolution chain:
//  1. Per-call parameters -- only if BOTH callName and callEmail are non-empty.
//     If only one is provided, return CodeInvalidArgument.
//  2. Init-time config (s.gitAuthorName / s.gitAuthorEmail) -- only if both set.
//  3. .gitconfig (repo-local then global) via go-git's config reader.
//  4. Error (CodeInvalidArgument) if none found.
func (s *Service) resolveAuthor(repoPath, callName, callEmail string) (string, string, error) {
	// Level 1: per-call
	if callName != "" || callEmail != "" {
		if callName == "" || callEmail == "" {
			return "", "", connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("both author_name and author_email must be provided"))
		}
		return callName, callEmail, nil
	}

	// Level 2: init-time config
	if s.gitAuthorName != "" && s.gitAuthorEmail != "" {
		return s.gitAuthorName, s.gitAuthorEmail, nil
	}

	// Level 3: .gitconfig via go-git
	if repoPath != "" {
		repo, err := git.PlainOpen(repoPath)
		if err == nil {
			// Try local scope first
			cfg, err := repo.ConfigScoped(config.LocalScope)
			if err == nil && cfg.User.Name != "" && cfg.User.Email != "" {
				return cfg.User.Name, cfg.User.Email, nil
			}
			// Try global scope
			cfg, err = repo.ConfigScoped(config.GlobalScope)
			if err == nil && cfg.User.Name != "" && cfg.User.Email != "" {
				return cfg.User.Name, cfg.User.Email, nil
			}
		}
	}

	// Level 4: error
	return "", "", connect.NewError(connect.CodeInvalidArgument,
		fmt.Errorf("git author name and email are required"))
}

// gitCommit stages files and creates a commit using go-git.
// If files is empty, commits all currently staged changes.
// If files is specified, stages those files first then commits.
// Returns the commit SHA.
func (s *Service) gitCommit(ctx context.Context, repoPath, message string, files []string, authorName, authorEmail string) (string, error) {
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

	repo, err := git.PlainOpen(absRepoPath)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryNotExists) {
			return "", connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("not a git repository"))
		}
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("open repository: %w", err))
	}

	wt, err := repo.Worktree()
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("get worktree: %w", err))
	}

	// Stage specified files
	if len(files) > 0 {
		for _, file := range files {
			_, err := ValidatePath(s.homeDir, file)
			if err == ErrPathTraversal {
				return "", connect.NewError(connect.CodePermissionDenied,
					fmt.Errorf("file path traversal: %s", file))
			}

			if _, err := wt.Add(file); err != nil {
				return "", connect.NewError(connect.CodeInternal,
					fmt.Errorf("git add %s: %w", file, err))
			}
		}
	}

	// Check for staged changes
	status, err := wt.Status()
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("worktree status: %w", err))
	}

	hasStagedChanges := false
	for _, fs := range status {
		if fs.Staging != git.Unmodified && fs.Staging != git.Untracked {
			hasStagedChanges = true
			break
		}
	}
	if !hasStagedChanges {
		return "", connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("nothing to commit"))
	}

	// Resolve author
	name, email, err := s.resolveAuthor(absRepoPath, authorName, authorEmail)
	if err != nil {
		return "", err
	}

	hash, err := wt.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  name,
			Email: email,
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("git commit: %w", err))
	}

	return hash.String(), nil
}
