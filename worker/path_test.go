package worker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePath_RejectsTraversalAttempts(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()

	tests := []struct {
		name string
		path string
	}{
		{"parent traversal", "../../../etc/passwd"},
		{"parent traversal with leading dot", "./../../../etc/passwd"},
		{"parent traversal embedded", "subdir/../../../etc/passwd"},
		{"double dot in middle", "foo/../../bar/../../../etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidatePath(homeDir, tt.path)
			assert.ErrorIs(t, err, ErrPathTraversal)
		})
	}
}

func TestValidatePath_RejectsAbsolutePathsOutsideHome(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()

	tests := []struct {
		name string
		path string
	}{
		{"etc passwd", "/etc/passwd"},
		{"tmp", "/tmp/something"},
		{"root", "/"},
		{"similar prefix but different dir", homeDir + "2/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ValidatePath(homeDir, tt.path)
			assert.ErrorIs(t, err, ErrPathTraversal)
		})
	}
}

func TestValidatePath_AcceptsValidPaths(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	// Resolve the temp dir to handle /var -> /private/var on macOS
	realHomeDir, err := filepath.EvalSymlinks(homeDir)
	require.NoError(t, err)

	// Create some files and directories
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "subdir", "nested"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "file.txt"), []byte("test"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(homeDir, "subdir", "file.txt"), []byte("test"), 0644))

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"simple file", "file.txt", filepath.Join(realHomeDir, "file.txt")},
		{"nested file", "subdir/file.txt", filepath.Join(realHomeDir, "subdir", "file.txt")},
		{"directory", "subdir", filepath.Join(realHomeDir, "subdir")},
		{"deeply nested", "subdir/nested", filepath.Join(realHomeDir, "subdir", "nested")},
		{"absolute under home", filepath.Join(homeDir, "file.txt"), filepath.Join(realHomeDir, "file.txt")},
		{"empty path returns home", "", realHomeDir},
		{"current dir", ".", realHomeDir},
		{"traversal that stays inside", "subdir/../file.txt", filepath.Join(realHomeDir, "file.txt")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := ValidatePath(homeDir, tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidatePath_AcceptsNonExistentPaths(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()

	// Create a parent directory
	require.NoError(t, os.MkdirAll(filepath.Join(homeDir, "existing"), 0755))

	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"new file in home", "newfile.txt", filepath.Join(homeDir, "newfile.txt")},
		{"new file in existing dir", "existing/newfile.txt", filepath.Join(homeDir, "existing", "newfile.txt")},
		{"new dir", "newdir/", filepath.Join(homeDir, "newdir")},
		{"deeply nested new path", "new/deep/path/file.txt", filepath.Join(homeDir, "new", "deep", "path", "file.txt")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := ValidatePath(homeDir, tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidatePath_RejectsSymlinkEscapeAttempts(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()

	// Create a symlink that points outside the home directory
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	require.NoError(t, os.WriteFile(outsideFile, []byte("secret"), 0644))

	// Create symlink inside home pointing to outside
	symlinkPath := filepath.Join(homeDir, "escape")
	require.NoError(t, os.Symlink(outsideDir, symlinkPath))

	t.Run("symlink to outside directory", func(t *testing.T) {
		t.Parallel()
		_, err := ValidatePath(homeDir, "escape")
		assert.ErrorIs(t, err, ErrPathTraversal)
	})

	t.Run("file through symlink to outside", func(t *testing.T) {
		t.Parallel()
		_, err := ValidatePath(homeDir, "escape/secret.txt")
		assert.ErrorIs(t, err, ErrPathTraversal)
	})
}

func TestValidatePath_AcceptsSymlinksWithinHome(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	// Resolve temp dir for /var -> /private/var on macOS
	realHomeDir, err := filepath.EvalSymlinks(homeDir)
	require.NoError(t, err)

	// Create a valid structure
	targetDir := filepath.Join(realHomeDir, "target")
	require.NoError(t, os.MkdirAll(targetDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "file.txt"), []byte("test"), 0644))

	// Create symlink within home directory
	symlinkPath := filepath.Join(homeDir, "link")
	require.NoError(t, os.Symlink(targetDir, symlinkPath))

	t.Run("symlink within home", func(t *testing.T) {
		t.Parallel()
		result, err := ValidatePath(homeDir, "link")
		require.NoError(t, err)
		assert.Equal(t, targetDir, result)
	})

	t.Run("file through symlink within home", func(t *testing.T) {
		t.Parallel()
		result, err := ValidatePath(homeDir, "link/file.txt")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(targetDir, "file.txt"), result)
	})
}

func TestValidatePath_HandlesRelativeSymlinkParent(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	outsideDir := t.TempDir()

	// Create a directory inside home with a symlink parent pointing outside
	subdir := filepath.Join(homeDir, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0755))

	// Create symlink from inside to outside
	symlinkInSub := filepath.Join(subdir, "escape")
	require.NoError(t, os.Symlink(outsideDir, symlinkInSub))

	t.Run("nested symlink escape", func(t *testing.T) {
		t.Parallel()
		_, err := ValidatePath(homeDir, "subdir/escape/anything")
		assert.ErrorIs(t, err, ErrPathTraversal)
	})
}
