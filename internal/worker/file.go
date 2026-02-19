package worker

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"
	"github.com/bmatcuk/doublestar/v4"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
)

// ReadFile reads file content with optional offset and limit.
// Returns connect error codes:
//   - CodeNotFound: file doesn't exist
//   - CodePermissionDenied: path traversal attempt
//   - CodeInvalidArgument: path is a directory
func (s *Service) readFile(ctx context.Context, path string, offset, limit int64) ([]byte, error) {
	absPath, err := ValidatePath(s.homeDir, path)
	if err == ErrPathTraversal {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("path validation failed: %w", err))
	}

	// Check if path is a directory
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found: %s", path))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stat file: %w", err))
	}
	if info.IsDir() {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is a directory: %s", path))
	}

	f, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found: %s", path))
		}
		if os.IsPermission(err) {
			return nil, connect.NewError(connect.CodePermissionDenied, fmt.Errorf("permission denied: %s", path))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to open file: %w", err))
	}
	defer func() { _ = f.Close() }()

	// Handle offset
	if offset > 0 {
		if _, err := f.Seek(offset, io.SeekStart); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to seek: %w", err))
		}
	}

	// Read content
	var reader io.Reader = f
	if limit > 0 {
		reader = io.LimitReader(f, limit)
	}

	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read file: %w", err))
	}

	return content, nil
}

// WriteFile writes content to a file, creating parent directories as needed.
// Returns connect error codes:
//   - CodePermissionDenied: path traversal attempt
//   - CodeInvalidArgument: malformed request
func (s *Service) writeFile(ctx context.Context, path string, content []byte, mode int32) error {
	if path == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is required"))
	}

	absPath, err := ValidatePath(s.homeDir, path)
	if err == ErrPathTraversal {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("path validation failed: %w", err))
	}

	// Default mode to 0644 if not specified
	fileMode := fs.FileMode(0644)
	if mode > 0 {
		fileMode = fs.FileMode(mode)
	}

	// Create parent directories if needed
	parentDir := filepath.Dir(absPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create parent directories: %w", err))
	}

	// Write the file
	if err := os.WriteFile(absPath, content, fileMode); err != nil {
		if os.IsPermission(err) {
			return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("permission denied: %s", path))
		}
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to write file: %w", err))
	}

	// Explicitly set mode in case file already existed with different permissions
	if err := os.Chmod(absPath, fileMode); err != nil {
		// Log but don't fail - the write succeeded
		s.logger.Warn("failed to set file mode", "path", path, "mode", fileMode, "error", err)
	}

	return nil
}

// DeleteFile deletes a file or directory.
// Returns connect error codes:
//   - CodeNotFound: file doesn't exist
//   - CodePermissionDenied: path traversal attempt
//   - CodeFailedPrecondition: non-empty directory without recursive flag
func (s *Service) deleteFile(ctx context.Context, path string, recursive bool) error {
	if path == "" {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is required"))
	}

	absPath, err := ValidatePath(s.homeDir, path)
	if err == ErrPathTraversal {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("path validation failed: %w", err))
	}

	// Don't allow deleting the home directory itself
	if absPath == s.homeDir {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("cannot delete home directory"))
	}

	info, err := os.Lstat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return connect.NewError(connect.CodeNotFound, fmt.Errorf("file not found: %s", path))
		}
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stat file: %w", err))
	}

	if info.IsDir() && !recursive {
		// Check if directory is empty
		entries, err := os.ReadDir(absPath)
		if err != nil {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read directory: %w", err))
		}
		if len(entries) > 0 {
			return connect.NewError(connect.CodeFailedPrecondition,
				fmt.Errorf("directory not empty, use recursive=true to delete: %s", path))
		}
	}

	if recursive {
		if err := os.RemoveAll(absPath); err != nil {
			if os.IsPermission(err) {
				return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("permission denied: %s", path))
			}
			return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete: %w", err))
		}
	} else {
		if err := os.Remove(absPath); err != nil {
			if os.IsPermission(err) {
				return connect.NewError(connect.CodePermissionDenied, fmt.Errorf("permission denied: %s", path))
			}
			return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to delete: %w", err))
		}
	}

	return nil
}

// listFiles lists files at a path, optionally recursively.
// Returns connect error codes:
//   - CodeNotFound: path doesn't exist
//   - CodePermissionDenied: path traversal attempt
//   - CodeInvalidArgument: path is not a directory
func (s *Service) listFiles(ctx context.Context, path string, recursive bool) ([]*remotehandsv1.FileEntry, error) {
	absPath, err := ValidatePath(s.homeDir, path)
	if err == ErrPathTraversal {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("path validation failed: %w", err))
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("path not found: %s", path))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stat path: %w", err))
	}
	if !info.IsDir() {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is not a directory: %s", path))
	}

	// Get symlink-resolved home directory for relative path computation
	// ValidatePath returns symlink-resolved paths, so we need to resolve homeDir too
	resolvedHomeDir, err := filepath.EvalSymlinks(s.homeDir)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to resolve home directory: %w", err))
	}

	var entries []*remotehandsv1.FileEntry

	if recursive {
		err = filepath.WalkDir(absPath, func(walkPath string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				// Skip paths with errors, log and continue
				s.logger.Warn("failed to access path during walk", "path", walkPath, "error", walkErr)
				return nil
			}
			// Skip the root directory itself
			if walkPath == absPath {
				return nil
			}

			entry, entryErr := fileEntryFromPath(resolvedHomeDir, walkPath, d)
			if entryErr != nil {
				s.logger.Warn("failed to get file info", "path", walkPath, "error", entryErr)
				return nil
			}
			entries = append(entries, entry)
			return nil
		})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to walk directory: %w", err))
		}
	} else {
		dirEntries, err := os.ReadDir(absPath)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to read directory: %w", err))
		}

		for _, d := range dirEntries {
			entryPath := filepath.Join(absPath, d.Name())
			entry, entryErr := fileEntryFromPath(resolvedHomeDir, entryPath, d)
			if entryErr != nil {
				s.logger.Warn("failed to get file info", "path", entryPath, "error", entryErr)
				continue
			}
			entries = append(entries, entry)
		}
	}

	return entries, nil
}

// fileEntryFromPath creates a FileEntry from a path and DirEntry.
// baseDir should be the resolved (symlink-evaluated) base directory.
func fileEntryFromPath(baseDir, absPath string, d fs.DirEntry) (*remotehandsv1.FileEntry, error) {
	info, err := d.Info()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// Calculate relative path from base directory
	relPath, err := filepath.Rel(baseDir, absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to compute relative path: %w", err)
	}

	entryType := "file"
	if info.IsDir() {
		entryType = "directory"
	} else if info.Mode()&os.ModeSymlink != 0 {
		entryType = "symlink"
	}

	return &remotehandsv1.FileEntry{
		Path:       relPath,
		Type:       entryType,
		Size:       info.Size(),
		ModifiedAt: info.ModTime().Unix(),
		Mode:       fmt.Sprintf("%04o", info.Mode().Perm()),
	}, nil
}

// glob matches files against a glob pattern.
// Returns connect error codes:
//   - CodeNotFound: base path doesn't exist
//   - CodePermissionDenied: path traversal attempt
//   - CodeInvalidArgument: invalid glob pattern
func (s *Service) glob(ctx context.Context, pattern, basePath string) ([]string, error) {
	// Validate base path
	absBasePath, err := ValidatePath(s.homeDir, basePath)
	if err == ErrPathTraversal {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("path validation failed: %w", err))
	}

	info, err := os.Stat(absBasePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("path not found: %s", basePath))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to stat path: %w", err))
	}
	if !info.IsDir() {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("path is not a directory: %s", basePath))
	}

	// Validate pattern syntax
	if !doublestar.ValidatePattern(pattern) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid glob pattern: %s", pattern))
	}

	// Use doublestar for ** support
	var matches []string
	err = doublestar.GlobWalk(os.DirFS(absBasePath), pattern, func(path string, d fs.DirEntry) error {
		// doublestar returns paths relative to the FS root (absBasePath)
		matches = append(matches, path)
		return nil
	})
	if err != nil {
		// Check if it's a pattern error
		if strings.Contains(err.Error(), "pattern") {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid glob pattern: %w", err))
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("glob failed: %w", err))
	}

	return matches, nil
}
