package worker

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"golang.org/x/crypto/ssh"
)

// ServiceGitOptions configures git authentication and commit author defaults
// for a Service created via NewServiceWithGitAuth.
type ServiceGitOptions struct {
	SSHKey      string // PEM-encoded private key (empty = no SSH auth)
	HTTPSToken  string // PAT or OAuth token (empty = no HTTPS auth)
	AuthorName  string // default author name for commits
	AuthorEmail string // default author email for commits
}

// NewServiceWithGitAuth creates a Service with git credentials loaded into memory.
// Credentials are never written to disk -- stored as go-git transport.AuthMethod objects.
func NewServiceWithGitAuth(homeDir string, logger *slog.Logger, opts ServiceGitOptions) (*Service, error) {
	svc, err := NewService(homeDir, logger)
	if err != nil {
		return nil, err
	}

	if opts.SSHKey != "" {
		auth, err := gitssh.NewPublicKeys("git", []byte(opts.SSHKey), "")
		if err != nil {
			return nil, fmt.Errorf("parse SSH key: %w", err)
		}
		// Accept any host key — typical for deploy key usage.
		// nolint:gosec // Intentional for automated deploy key workflows.
		auth.HostKeyCallback = ssh.InsecureIgnoreHostKey()
		svc.gitSSHAuth = auth
	}

	if opts.HTTPSToken != "" {
		svc.gitHTTPSAuth = &githttp.BasicAuth{
			Username: "x-token",
			Password: opts.HTTPSToken,
		}
	}

	svc.gitAuthorName = opts.AuthorName
	svc.gitAuthorEmail = opts.AuthorEmail

	return svc, nil
}

// selectGitAuth returns the appropriate auth method based on the repo URL scheme.
func (s *Service) selectGitAuth(repoURL string) transport.AuthMethod {
	if strings.HasPrefix(repoURL, "git@") || strings.HasPrefix(repoURL, "ssh://") {
		return s.gitSSHAuth
	}
	if strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "http://") {
		return s.gitHTTPSAuth
	}
	return nil // local path or unknown scheme
}

// gitClone clones a remote repository into a path under homeDir.
// depth: 0 = default (shallow depth 1), -1 = full history, >0 = specific depth.
func (s *Service) gitClone(ctx context.Context, repoURL, localPath, branch string, depth int32) (string, error) {
	if repoURL == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("repo_url is required"))
	}

	absPath, err := ValidatePath(s.homeDir, localPath)
	if err == ErrPathTraversal {
		return "", connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		// If the path doesn't exist yet (expected for clone), resolve it
		// relative to homeDir manually.
		absPath = filepath.Join(s.homeDir, localPath)
	}

	// Resolve clone depth: 0 = default shallow (1), -1 = full, >0 = explicit.
	cloneDepth := 1
	if depth > 0 {
		cloneDepth = int(depth)
	} else if depth == -1 {
		cloneDepth = 0 // go-git: 0 means no depth limit (full clone)
	}

	opts := &git.CloneOptions{
		URL:   repoURL,
		Auth:  s.selectGitAuth(repoURL),
		Depth: cloneDepth,
	}

	if branch != "" {
		opts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		opts.SingleBranch = true
	}

	repo, err := git.PlainCloneContext(ctx, absPath, false, opts)
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("git clone: %w", err))
	}

	head, err := repo.Head()
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("get HEAD after clone: %w", err))
	}

	return head.Hash().String(), nil
}

// gitPush pushes a local repository's branch to the remote.
func (s *Service) gitPush(ctx context.Context, repoPath, remoteName, branch string, force bool) error {
	absPath, err := ValidatePath(s.homeDir, repoPath)
	if err == ErrPathTraversal {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("path validation failed: %w", err))
	}

	repo, err := git.PlainOpen(absPath)
	if err != nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("open repository: %w", err))
	}

	if remoteName == "" {
		remoteName = "origin"
	}

	// Determine the remote URL for auth selection.
	remote, err := repo.Remote(remoteName)
	if err != nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("get remote %q: %w", remoteName, err))
	}
	urls := remote.Config().URLs
	var auth transport.AuthMethod
	if len(urls) > 0 {
		auth = s.selectGitAuth(urls[0])
	}

	opts := &git.PushOptions{
		RemoteName: remoteName,
		Auth:       auth,
		Force:      force,
	}

	if branch != "" {
		refSpec := config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch))
		if force {
			refSpec = config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", branch, branch))
		}
		opts.RefSpecs = []config.RefSpec{refSpec}
	}

	if err := repo.PushContext(ctx, opts); err != nil {
		if err == git.NoErrAlreadyUpToDate {
			return nil // not an error
		}
		return connect.NewError(connect.CodeInternal, fmt.Errorf("git push: %w", err))
	}

	return nil
}
