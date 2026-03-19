package worker

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"connectrpc.com/connect"
	"github.com/bmatcuk/doublestar/v4"
	"github.com/fsnotify/fsnotify"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
)

// watchFiles watches files matching the given glob patterns and streams events.
// The stream continues until context cancellation or an error occurs.
// Returns connect error codes:
//   - CodeInvalidArgument: empty patterns or invalid glob pattern
//   - CodePermissionDenied: path traversal attempt
//   - CodeInternal: watcher creation or other internal errors
func (s *Service) watchFiles(
	ctx context.Context,
	patterns []string,
	send func(*remotehandsv1.FileEvent) error,
) error {
	if len(patterns) == 0 {
		return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("at least one pattern is required"))
	}

	// Validate all patterns first
	for _, pattern := range patterns {
		if !doublestar.ValidatePattern(pattern) {
			return connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid glob pattern: %s", pattern))
		}
	}

	// Get symlink-resolved home directory
	resolvedHomeDir, err := filepath.EvalSymlinks(s.homeDir)
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to resolve home directory: %w", err))
	}

	// Create the watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to create watcher: %w", err))
	}
	defer func() { _ = watcher.Close() }()

	// Collect directories to watch - we watch directories, not individual files,
	// because watching files directly doesn't catch new files matching patterns
	dirsToWatch := make(map[string]struct{})

	// Always watch the home directory for new files
	dirsToWatch[resolvedHomeDir] = struct{}{}

	// Walk the file tree and collect directories containing matches
	err = filepath.WalkDir(resolvedHomeDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			s.logger.Warn("failed to access path during watch setup", "path", path, "error", walkErr)
			return nil
		}

		// Always add directories to watch for detecting new files
		if d.IsDir() {
			dirsToWatch[path] = struct{}{}
			return nil
		}

		// Check if this file matches any pattern
		relPath, err := filepath.Rel(resolvedHomeDir, path)
		if err != nil {
			return nil
		}

		for _, pattern := range patterns {
			matched, err := doublestar.Match(pattern, relPath)
			if err != nil {
				continue
			}
			if matched {
				// Add the parent directory to watch list
				dirsToWatch[filepath.Dir(path)] = struct{}{}
				break
			}
		}

		return nil
	})
	if err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to walk directory: %w", err))
	}

	// Add all directories to the watcher
	for dir := range dirsToWatch {
		if err := watcher.Add(dir); err != nil {
			s.logger.Warn("failed to watch directory", "path", dir, "error", err)
		}
	}

	s.logger.Debug("watching directories", "count", len(dirsToWatch))

	// Event loop
	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			// Validate the event path is still within the home directory
			if !isUnderDir(resolvedHomeDir, event.Name) {
				continue
			}

			// Get relative path from home directory
			relPath, err := filepath.Rel(resolvedHomeDir, event.Name)
			if err != nil {
				s.logger.Warn("failed to compute relative path", "path", event.Name, "error", err)
				continue
			}

			// Check if the event matches any pattern
			matched := false
			for _, pattern := range patterns {
				m, err := doublestar.Match(pattern, relPath)
				if err == nil && m {
					matched = true
					break
				}
			}
			if !matched {
				// If a new directory was created, add it to the watcher
				if event.Has(fsnotify.Create) {
					info, err := os.Stat(event.Name)
					if err == nil && info.IsDir() {
						if err := watcher.Add(event.Name); err != nil {
							s.logger.Warn("failed to add new directory to watcher", "path", event.Name, "error", err)
						}
					}
				}
				continue
			}

			// Map fsnotify event to our event type
			eventType := mapFsnotifyEvent(event.Op)
			if eventType == "" {
				continue
			}

			fileEvent := &remotehandsv1.FileEvent{
				Path:      relPath,
				EventType: eventType,
			}

			if err := send(fileEvent); err != nil {
				s.logger.Debug("failed to send event, stream likely closed", "error", err)
				return nil
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			s.logger.Error("watcher error", "error", err)
			// Don't return on transient errors, just log them
		}
	}
}

// mapFsnotifyEvent maps fsnotify operations to our event type strings.
// Returns empty string for events we don't report.
func mapFsnotifyEvent(op fsnotify.Op) string {
	switch {
	case op.Has(fsnotify.Create):
		return "created"
	case op.Has(fsnotify.Write):
		return "modified"
	case op.Has(fsnotify.Remove):
		return "deleted"
	case op.Has(fsnotify.Rename):
		// Rename is treated as delete of the old name
		// The new name will come as a separate Create event
		return "deleted"
	default:
		return ""
	}
}
