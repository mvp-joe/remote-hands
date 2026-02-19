package worker

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	remotehandsv1 "github.com/mvp-joe/remote-hands/gen/remotehands/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fileEventCollector collects FileEvents for testing
type fileEventCollector struct {
	mu     sync.Mutex
	events []*remotehandsv1.FileEvent
}

func (c *fileEventCollector) send(event *remotehandsv1.FileEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
	return nil
}

func (c *fileEventCollector) getEvents() []*remotehandsv1.FileEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]*remotehandsv1.FileEvent, len(c.events))
	copy(result, c.events)
	return result
}

func (c *fileEventCollector) waitForEvent(t *testing.T, timeout time.Duration, predicate func(*remotehandsv1.FileEvent) bool) *remotehandsv1.FileEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		events := c.getEvents()
		for _, e := range events {
			if predicate(e) {
				return e
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for event, collected events: %+v", c.getEvents())
	return nil
}

// =============================================================================
// WatchFiles Tests
// =============================================================================

func TestService_WatchFiles_DetectsCreatedFile(t *testing.T) {
	t.Parallel()
	svc, homeDir := newTestService(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := &fileEventCollector{}

	// Start watching in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.watchFiles(ctx, []string{"*.txt"}, collector.send)
	}()

	// Give the watcher time to set up
	time.Sleep(100 * time.Millisecond)

	// Create a file
	err := os.WriteFile(filepath.Join(homeDir, "test.txt"), []byte("hello"), 0644)
	require.NoError(t, err)

	// Wait for the created event
	event := collector.waitForEvent(t, 2*time.Second, func(e *remotehandsv1.FileEvent) bool {
		return e.Path == "test.txt" && e.EventType == "created"
	})
	assert.Equal(t, "test.txt", event.Path)
	assert.Equal(t, "created", event.EventType)

	// Cancel and verify clean shutdown
	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("watchFiles did not return after context cancellation")
	}
}

func TestService_WatchFiles_DetectsModifiedFile(t *testing.T) {
	t.Parallel()
	svc, homeDir := newTestService(t)

	// Create file first
	filePath := filepath.Join(homeDir, "existing.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("initial"), 0644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := &fileEventCollector{}

	// Start watching
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.watchFiles(ctx, []string{"*.txt"}, collector.send)
	}()

	time.Sleep(100 * time.Millisecond)

	// Modify the file
	require.NoError(t, os.WriteFile(filePath, []byte("modified content"), 0644))

	// Wait for the modified event
	event := collector.waitForEvent(t, 2*time.Second, func(e *remotehandsv1.FileEvent) bool {
		return e.Path == "existing.txt" && e.EventType == "modified"
	})
	assert.Equal(t, "existing.txt", event.Path)
	assert.Equal(t, "modified", event.EventType)

	cancel()
}

func TestService_WatchFiles_DetectsDeletedFile(t *testing.T) {
	t.Parallel()
	svc, homeDir := newTestService(t)

	// Create file first
	filePath := filepath.Join(homeDir, "todelete.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0644))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := &fileEventCollector{}

	// Start watching
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.watchFiles(ctx, []string{"*.txt"}, collector.send)
	}()

	time.Sleep(100 * time.Millisecond)

	// Delete the file
	require.NoError(t, os.Remove(filePath))

	// Wait for the deleted event
	event := collector.waitForEvent(t, 2*time.Second, func(e *remotehandsv1.FileEvent) bool {
		return e.Path == "todelete.txt" && e.EventType == "deleted"
	})
	assert.Equal(t, "todelete.txt", event.Path)
	assert.Equal(t, "deleted", event.EventType)

	cancel()
}

func TestService_WatchFiles_GlobPatternFilters(t *testing.T) {
	t.Parallel()
	svc, homeDir := newTestService(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := &fileEventCollector{}

	// Start watching only .go files
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.watchFiles(ctx, []string{"*.go"}, collector.send)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create a .txt file (should be ignored)
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "ignored.txt"), []byte("x"), 0644))

	// Create a .go file (should be reported)
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "main.go"), []byte("package main"), 0644))

	// Wait for the .go file event
	event := collector.waitForEvent(t, 2*time.Second, func(e *remotehandsv1.FileEvent) bool {
		return e.Path == "main.go" && e.EventType == "created"
	})
	assert.Equal(t, "main.go", event.Path)

	// Verify no event for the .txt file
	time.Sleep(200 * time.Millisecond)
	events := collector.getEvents()
	for _, e := range events {
		assert.NotEqual(t, "ignored.txt", e.Path, "should not receive event for non-matching file")
	}

	cancel()
}

func TestService_WatchFiles_DoubleStarPattern(t *testing.T) {
	t.Parallel()
	svc, homeDir := newTestService(t)

	// Create nested directory structure
	nestedDir := filepath.Join(homeDir, "a", "b", "c")
	require.NoError(t, os.MkdirAll(nestedDir, 0755))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := &fileEventCollector{}

	// Watch all .go files recursively
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.watchFiles(ctx, []string{"**/*.go"}, collector.send)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create a deeply nested .go file
	require.NoError(t, os.WriteFile(filepath.Join(nestedDir, "deep.go"), []byte("package deep"), 0644))

	// Wait for the event
	event := collector.waitForEvent(t, 2*time.Second, func(e *remotehandsv1.FileEvent) bool {
		return e.EventType == "created" && e.Path == filepath.Join("a", "b", "c", "deep.go")
	})
	assert.Equal(t, filepath.Join("a", "b", "c", "deep.go"), event.Path)
	assert.Equal(t, "created", event.EventType)

	cancel()
}

func TestService_WatchFiles_MultiplePatterns(t *testing.T) {
	t.Parallel()
	svc, homeDir := newTestService(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := &fileEventCollector{}

	// Watch both .go and .ts files
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.watchFiles(ctx, []string{"*.go", "*.ts"}, collector.send)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create both file types
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "main.go"), []byte("package main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "app.ts"), []byte("const x = 1"), 0644))

	// Wait for both events
	collector.waitForEvent(t, 2*time.Second, func(e *remotehandsv1.FileEvent) bool {
		return e.Path == "main.go" && e.EventType == "created"
	})
	collector.waitForEvent(t, 2*time.Second, func(e *remotehandsv1.FileEvent) bool {
		return e.Path == "app.ts" && e.EventType == "created"
	})

	cancel()
}

func TestService_WatchFiles_EmptyPatternsReturnsError(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)

	ctx := context.Background()
	collector := &fileEventCollector{}

	err := svc.watchFiles(ctx, []string{}, collector.send)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

func TestService_WatchFiles_InvalidPatternReturnsError(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)

	ctx := context.Background()
	collector := &fileEventCollector{}

	err := svc.watchFiles(ctx, []string{"[invalid"}, collector.send)
	require.Error(t, err)

	var connErr *connect.Error
	require.ErrorAs(t, err, &connErr)
	assert.Equal(t, connect.CodeInvalidArgument, connErr.Code())
}

func TestService_WatchFiles_DetectsNewDirectoryAndFiles(t *testing.T) {
	t.Parallel()
	svc, homeDir := newTestService(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	collector := &fileEventCollector{}

	// Watch all .txt files recursively
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.watchFiles(ctx, []string{"**/*.txt"}, collector.send)
	}()

	time.Sleep(100 * time.Millisecond)

	// Create a new directory and file in it
	newDir := filepath.Join(homeDir, "newdir")
	require.NoError(t, os.MkdirAll(newDir, 0755))

	// Give watcher time to detect and add the new directory
	time.Sleep(100 * time.Millisecond)

	// Create a file in the new directory
	require.NoError(t, os.WriteFile(filepath.Join(newDir, "newfile.txt"), []byte("content"), 0644))

	// Wait for the event
	event := collector.waitForEvent(t, 2*time.Second, func(e *remotehandsv1.FileEvent) bool {
		return e.Path == filepath.Join("newdir", "newfile.txt") && e.EventType == "created"
	})
	assert.Equal(t, filepath.Join("newdir", "newfile.txt"), event.Path)

	cancel()
}

func TestService_WatchFiles_ContextCancellation(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)

	ctx, cancel := context.WithCancel(context.Background())
	collector := &fileEventCollector{}

	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.watchFiles(ctx, []string{"*.txt"}, collector.send)
	}()

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Cancel the context
	cancel()

	// Should return without error
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("watchFiles did not return after context cancellation")
	}
}
