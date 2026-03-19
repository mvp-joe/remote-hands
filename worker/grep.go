package worker

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"connectrpc.com/connect"
	"github.com/bmatcuk/doublestar/v4"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
)

// grep searches file contents with a regex pattern.
// Returns connect error codes:
//   - CodeNotFound: path doesn't exist
//   - CodePermissionDenied: path traversal attempt
//   - CodeInvalidArgument: invalid regex pattern
func (s *Service) grep(
	ctx context.Context,
	pattern, path, globFilter string,
	ignoreCase bool,
	contextLines int32,
) ([]*remotehandsv1.GrepMatch, error) {
	// Validate path
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

	// Try ripgrep first, fall back to Go implementation
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return s.grepWithRipgrep(ctx, rgPath, absPath, pattern, globFilter, ignoreCase, contextLines)
	}

	return s.grepWithGo(ctx, absPath, pattern, globFilter, ignoreCase, contextLines)
}

// grepWithRipgrep uses ripgrep for searching.
func (s *Service) grepWithRipgrep(
	ctx context.Context,
	rgPath, absPath, pattern, globFilter string,
	ignoreCase bool,
	contextLines int32,
) ([]*remotehandsv1.GrepMatch, error) {
	args := []string{
		"--json",        // JSON output for structured parsing
		"--no-heading",  // Don't group by file
		"--line-number", // Include line numbers
	}

	if ignoreCase {
		args = append(args, "-i")
	}

	if contextLines > 0 {
		args = append(args, "-C", fmt.Sprintf("%d", contextLines))
	}

	if globFilter != "" {
		args = append(args, "--glob", globFilter)
	}

	args = append(args, pattern, absPath)

	cmd := exec.CommandContext(ctx, rgPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	// ripgrep exits with 1 when no matches found, which is not an error for us
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				// No matches found
				return nil, nil
			}
			// Exit code 2 means error
			if exitErr.ExitCode() == 2 {
				// Check if it's a regex error
				errStr := stderr.String()
				if strings.Contains(errStr, "regex") || strings.Contains(errStr, "parse") {
					return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid regex pattern: %s", pattern))
				}
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ripgrep error: %s", errStr))
			}
		}
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("ripgrep failed: %w", err))
	}

	return s.parseRipgrepJSON(stdout.Bytes(), absPath)
}

// ripgrepMessage represents a single line of ripgrep JSON output.
type ripgrepMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// ripgrepMatch represents a match in ripgrep JSON output.
type ripgrepMatch struct {
	Path struct {
		Text string `json:"text"`
	} `json:"path"`
	Lines struct {
		Text string `json:"text"`
	} `json:"lines"`
	LineNumber int `json:"line_number"`
}

// ripgrepContext represents context lines in ripgrep JSON output.
type ripgrepContext struct {
	Path struct {
		Text string `json:"text"`
	} `json:"path"`
	Lines struct {
		Text string `json:"text"`
	} `json:"lines"`
	LineNumber int `json:"line_number"`
}

// parseRipgrepJSON parses ripgrep's JSON output into GrepMatch objects.
func (s *Service) parseRipgrepJSON(data []byte, basePath string) ([]*remotehandsv1.GrepMatch, error) {
	var matches []*remotehandsv1.GrepMatch
	matchMap := make(map[string]*remotehandsv1.GrepMatch) // key: "path:linenum"
	var currentMatch *remotehandsv1.GrepMatch

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg ripgrepMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "match":
			var m ripgrepMatch
			if err := json.Unmarshal(msg.Data, &m); err != nil {
				continue
			}

			relPath, err := filepath.Rel(basePath, m.Path.Text)
			if err != nil {
				relPath = m.Path.Text
			}

			match := &remotehandsv1.GrepMatch{
				Path:    relPath,
				Line:    int32(m.LineNumber),
				Content: strings.TrimRight(m.Lines.Text, "\n\r"),
			}

			key := fmt.Sprintf("%s:%d", m.Path.Text, m.LineNumber)
			matchMap[key] = match
			currentMatch = match
			matches = append(matches, match)

		case "context":
			var c ripgrepContext
			if err := json.Unmarshal(msg.Data, &c); err != nil {
				continue
			}

			if currentMatch == nil {
				continue
			}

			contextText := strings.TrimRight(c.Lines.Text, "\n\r")

			// Determine if this is before or after context based on line number
			if c.LineNumber < int(currentMatch.Line) {
				currentMatch.ContextBefore = append(currentMatch.ContextBefore, contextText)
			} else if c.LineNumber > int(currentMatch.Line) {
				currentMatch.ContextAfter = append(currentMatch.ContextAfter, contextText)
			}

		case "begin":
			// Reset currentMatch when we start a new file
			currentMatch = nil
		}
	}

	return matches, scanner.Err()
}

// grepWithGo implements grep using Go's regexp and filepath.Walk.
func (s *Service) grepWithGo(
	ctx context.Context,
	absPath, pattern, globFilter string,
	ignoreCase bool,
	contextLines int32,
) ([]*remotehandsv1.GrepMatch, error) {
	// Compile the regex
	if ignoreCase {
		pattern = "(?i)" + pattern
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid regex pattern: %w", err))
	}

	var matches []*remotehandsv1.GrepMatch

	err = filepath.WalkDir(absPath, func(walkPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // Skip paths with errors
		}

		if d.IsDir() {
			return nil
		}

		// Apply glob filter if specified
		if globFilter != "" {
			relPath, err := filepath.Rel(absPath, walkPath)
			if err != nil {
				return nil
			}

			matched, err := doublestar.Match(globFilter, relPath)
			if err != nil || !matched {
				return nil
			}
		}

		// Search this file
		fileMatches, err := s.searchFile(ctx, walkPath, absPath, re, int(contextLines))
		if err != nil {
			s.logger.Warn("failed to search file", "path", walkPath, "error", err)
			return nil
		}

		matches = append(matches, fileMatches...)
		return nil
	})

	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to walk directory: %w", err))
	}

	return matches, nil
}

// searchFile searches a single file for regex matches.
func (s *Service) searchFile(
	ctx context.Context,
	filePath, basePath string,
	re *regexp.Regexp,
	contextLines int,
) ([]*remotehandsv1.GrepMatch, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	// Skip binary files (simple heuristic: check for null bytes in first 8KB)
	checkLen := min(len(content), 8192)
	if bytes.IndexByte(content[:checkLen], 0) != -1 {
		return nil, nil
	}

	lines := strings.Split(string(content), "\n")
	var matches []*remotehandsv1.GrepMatch

	relPath, err := filepath.Rel(basePath, filePath)
	if err != nil {
		relPath = filePath
	}

	for i, line := range lines {
		if re.MatchString(line) {
			match := &remotehandsv1.GrepMatch{
				Path:    relPath,
				Line:    int32(i + 1), // 1-indexed
				Content: line,
			}

			// Add context before
			if contextLines > 0 {
				start := max(0, i-contextLines)
				for j := start; j < i; j++ {
					match.ContextBefore = append(match.ContextBefore, lines[j])
				}
			}

			// Add context after
			if contextLines > 0 {
				end := min(len(lines), i+contextLines+1)
				for j := i + 1; j < end; j++ {
					match.ContextAfter = append(match.ContextAfter, lines[j])
				}
			}

			matches = append(matches, match)
		}
	}

	return matches, nil
}
