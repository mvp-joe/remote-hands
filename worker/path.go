// Package worker implements the remote-hands worker service.
package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathTraversal indicates an attempt to access a path outside the home directory.
var ErrPathTraversal = fmt.Errorf("path traversal attempt")

// ValidatePath validates that requestedPath resolves to a location within homeDir.
// It returns the cleaned absolute path if valid, or an error if the path would
// escape the home directory boundary.
//
// Security checks:
//   - Rejects paths containing ".." that escape homeDir
//   - Rejects absolute paths not under homeDir
//   - Follows symlinks to detect symlink-based escape attempts
func ValidatePath(homeDir, requestedPath string) (string, error) {
	// Ensure homeDir is absolute and clean
	homeDir, err := filepath.Abs(homeDir)
	if err != nil {
		return "", fmt.Errorf("invalid home directory: %w", err)
	}
	homeDir = filepath.Clean(homeDir)

	// Resolve homeDir symlinks to get the canonical path
	// This handles cases like /var -> /private/var on macOS
	realHomeDir, err := filepath.EvalSymlinks(homeDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}

	// Handle empty path as home directory
	if requestedPath == "" {
		return realHomeDir, nil
	}

	// Build the target path
	var targetPath string
	if filepath.IsAbs(requestedPath) {
		// Absolute paths must be under homeDir
		targetPath = filepath.Clean(requestedPath)
	} else {
		// Relative paths are resolved from homeDir
		targetPath = filepath.Clean(filepath.Join(homeDir, requestedPath))
	}

	// First check: does the cleaned path start with homeDir?
	// This catches obvious traversal attempts like "../../../etc/passwd"
	// Check against both original and resolved homeDir for flexibility
	if !isUnderDir(homeDir, targetPath) && !isUnderDir(realHomeDir, targetPath) {
		return "", ErrPathTraversal
	}

	// Second check: if the path exists, resolve symlinks and verify again
	// This catches symlink-based escape attempts
	realPath, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Path doesn't exist yet - that's fine for writes
			// But we still need to check the parent directory if it has symlinks
			return validateNonExistentPath(realHomeDir, targetPath)
		}
		// Other errors (permission denied, etc.) - return as-is since the
		// operation will fail anyway
		return "", fmt.Errorf("failed to evaluate path: %w", err)
	}

	// Check the resolved real path against resolved homeDir
	if !isUnderDir(realHomeDir, realPath) {
		return "", ErrPathTraversal
	}

	return realPath, nil
}

// validateNonExistentPath validates a path that doesn't exist yet.
// It walks up to find the first existing ancestor and validates that.
// realHomeDir should be the symlink-resolved home directory.
func validateNonExistentPath(realHomeDir, targetPath string) (string, error) {
	// Walk up the path to find an existing ancestor
	current := targetPath
	for current != realHomeDir && current != "/" && current != "." {
		parent := filepath.Dir(current)
		if parent == current {
			break
		}

		_, err := os.Lstat(parent)
		if err != nil {
			if os.IsNotExist(err) {
				current = parent
				continue
			}
			return "", fmt.Errorf("failed to stat parent: %w", err)
		}

		// Parent exists - resolve symlinks and check
		realParent, err := filepath.EvalSymlinks(parent)
		if err != nil {
			return "", fmt.Errorf("failed to evaluate symlink: %w", err)
		}
		if !isUnderDir(realHomeDir, realParent) {
			return "", ErrPathTraversal
		}

		// Parent is valid, return the original target path (not resolved,
		// since the file doesn't exist yet)
		return targetPath, nil
	}

	// We walked all the way to homeDir or root without finding an existing path
	// If we're still under homeDir, it's valid
	if isUnderDir(realHomeDir, targetPath) {
		return targetPath, nil
	}
	return "", ErrPathTraversal
}

// isUnderDir checks if childPath is under parentDir.
// Both paths must be cleaned absolute paths.
func isUnderDir(parentDir, childPath string) bool {
	// Ensure trailing separator for proper prefix matching
	// This prevents "/home/user2" matching "/home/user"
	parentWithSep := parentDir
	if !strings.HasSuffix(parentWithSep, string(filepath.Separator)) {
		parentWithSep += string(filepath.Separator)
	}

	// Child is under parent if it equals parent or has parent as prefix
	return childPath == parentDir || strings.HasPrefix(childPath, parentWithSep)
}
