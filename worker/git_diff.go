package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"connectrpc.com/connect"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	fdiff "github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/utils/diff"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// gitDiff computes a unified diff using go-git's object store.
// For unstaged (staged=false): compares index blobs against working tree files.
// For staged (staged=true): compares HEAD tree blobs against index blobs.
func (s *Service) gitDiff(ctx context.Context, repoPath, filePath string, staged bool) (string, error) {
	absRepoPath, err := ValidatePath(s.homeDir, repoPath)
	if err == ErrPathTraversal {
		return "", connect.NewError(connect.CodePermissionDenied, err)
	}
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("path validation failed: %w", err))
	}

	if filePath != "" {
		_, err := ValidatePath(s.homeDir, filePath)
		if err == ErrPathTraversal {
			return "", connect.NewError(connect.CodePermissionDenied, err)
		}
		if err != nil {
			return "", connect.NewError(connect.CodeInternal, fmt.Errorf("file path validation failed: %w", err))
		}
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

	status, err := wt.Status()
	if err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("worktree status: %w", err))
	}

	var filePatches []fdiff.FilePatch

	if staged {
		filePatches, err = s.buildStagedDiff(repo, status, absRepoPath, filePath)
	} else {
		filePatches, err = s.buildUnstagedDiff(repo, status, absRepoPath, filePath)
	}
	if err != nil {
		return "", err
	}

	if len(filePatches) == 0 {
		return "", nil
	}

	p := &patch{filePatches: filePatches}
	var buf bytes.Buffer
	if err := fdiff.NewUnifiedEncoder(&buf, fdiff.DefaultContextLines).Encode(p); err != nil {
		return "", connect.NewError(connect.CodeInternal, fmt.Errorf("encode diff: %w", err))
	}

	return buf.String(), nil
}

// buildUnstagedDiff builds file patches comparing index → working tree.
func (s *Service) buildUnstagedDiff(repo *git.Repository, status git.Status, absRepoPath, filterPath string) ([]fdiff.FilePatch, error) {
	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read index: %w", err))
	}

	var patches []fdiff.FilePatch

	for name, fs := range status {
		if fs.Worktree == git.Unmodified || fs.Worktree == git.Untracked {
			continue
		}

		if filterPath != "" && name != filterPath {
			continue
		}

		var fromContent string
		var fromBinary bool
		var fromHash plumbing.Hash

		// Find entry in index for "from" content.
		for _, entry := range idx.Entries {
			if entry.Name == name {
				fromHash = entry.Hash
				blob, blobErr := object.GetBlob(repo.Storer, entry.Hash)
				if blobErr != nil {
					break
				}
				objFile := object.NewFile(name, entry.Mode, blob)
				bin, binErr := objFile.IsBinary()
				if binErr != nil {
					break
				}
				if bin {
					fromBinary = true
				} else {
					content, cErr := objFile.Contents()
					if cErr != nil {
						break
					}
					fromContent = content
				}
				break
			}
		}

		var toContent string
		var toBinary bool
		var toHash plumbing.Hash

		if fs.Worktree == git.Deleted {
			// File was deleted — "to" is empty.
			toContent = ""
		} else {
			absFile := filepath.Join(absRepoPath, name)
			data, readErr := os.ReadFile(absFile)
			if readErr != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read working tree file %s: %w", name, readErr))
			}
			toBinary = isBinaryContent(data)
			if !toBinary {
				toContent = string(data)
			}
			toHash = plumbing.ComputeHash(plumbing.BlobObject, data)
		}

		fp := buildFilePatch(name, name, fromContent, toContent, fromHash, toHash, fromBinary || toBinary, fs.Worktree == git.Deleted)
		patches = append(patches, fp)
	}

	return patches, nil
}

// buildStagedDiff builds file patches comparing HEAD → index.
func (s *Service) buildStagedDiff(repo *git.Repository, status git.Status, absRepoPath, filterPath string) ([]fdiff.FilePatch, error) {
	// Get HEAD tree (may not exist for initial commits).
	var headTree *object.Tree
	head, err := repo.Head()
	if err == nil {
		commit, cErr := repo.CommitObject(head.Hash())
		if cErr != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get HEAD commit: %w", cErr))
		}
		tree, tErr := commit.Tree()
		if tErr != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("get HEAD tree: %w", tErr))
		}
		headTree = tree
	}

	idx, err := repo.Storer.Index()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("read index: %w", err))
	}

	var patches []fdiff.FilePatch

	for name, fs := range status {
		if fs.Staging == git.Unmodified || fs.Staging == git.Untracked {
			continue
		}

		if filterPath != "" && name != filterPath {
			continue
		}

		var fromContent string
		var fromBinary bool
		var fromHash plumbing.Hash

		// "from" = HEAD tree blob.
		if headTree != nil && fs.Staging != git.Added {
			treeFile, fErr := headTree.File(name)
			if fErr == nil {
				fromHash = treeFile.Hash
				bin, binErr := treeFile.IsBinary()
				if binErr == nil && bin {
					fromBinary = true
				} else if binErr == nil {
					content, cErr := treeFile.Contents()
					if cErr == nil {
						fromContent = content
					}
				}
			}
		}

		var toContent string
		var toBinary bool
		var toHash plumbing.Hash

		if fs.Staging == git.Deleted {
			// Staged deletion — "to" is empty.
			toContent = ""
		} else {
			// "to" = index blob.
			for _, entry := range idx.Entries {
				if entry.Name == name {
					toHash = entry.Hash
					blob, blobErr := object.GetBlob(repo.Storer, entry.Hash)
					if blobErr != nil {
						break
					}
					objFile := object.NewFile(name, entry.Mode, blob)
					bin, binErr := objFile.IsBinary()
					if binErr != nil {
						break
					}
					if bin {
						toBinary = true
					} else {
						content, cErr := objFile.Contents()
						if cErr != nil {
							break
						}
						toContent = content
					}
					break
				}
			}
		}

		isNew := fs.Staging == git.Added
		isDelete := fs.Staging == git.Deleted
		fp := buildFilePatchEx(name, fromContent, toContent, fromHash, toHash, fromBinary || toBinary, isNew, isDelete)
		patches = append(patches, fp)
	}

	return patches, nil
}

// buildFilePatch creates a filePatch for unstaged changes.
func buildFilePatch(fromPath, toPath, fromContent, toContent string, fromHash, toHash plumbing.Hash, binary, isDelete bool) *filePatch {
	var from, to fdiff.File
	from = &file{hash: fromHash, mode: filemode.Regular, path: fromPath}
	if isDelete {
		to = nil
	} else {
		to = &file{hash: toHash, mode: filemode.Regular, path: toPath}
	}

	if binary {
		return &filePatch{from: from, to: to, isBinary: true}
	}

	chunks := buildChunks(fromContent, toContent)
	return &filePatch{from: from, to: to, chunks: chunks}
}

// buildFilePatchEx creates a filePatch for staged changes, handling new/deleted files.
func buildFilePatchEx(name, fromContent, toContent string, fromHash, toHash plumbing.Hash, binary, isNew, isDelete bool) *filePatch {
	var from, to fdiff.File
	if isNew {
		from = nil
	} else {
		from = &file{hash: fromHash, mode: filemode.Regular, path: name}
	}
	if isDelete {
		to = nil
	} else {
		to = &file{hash: toHash, mode: filemode.Regular, path: name}
	}

	if binary {
		return &filePatch{from: from, to: to, isBinary: true}
	}

	chunks := buildChunks(fromContent, toContent)
	return &filePatch{from: from, to: to, chunks: chunks}
}

// buildChunks runs diff.Do and maps results to fdiff.Chunk slice.
func buildChunks(fromContent, toContent string) []fdiff.Chunk {
	diffs := diff.Do(fromContent, toContent)
	chunks := make([]fdiff.Chunk, 0, len(diffs))
	for _, d := range diffs {
		if d.Text == "" {
			continue
		}
		chunks = append(chunks, &chunk{
			content: d.Text,
			op:      mapDiffOp(d.Type),
		})
	}
	return chunks
}

// mapDiffOp converts diffmatchpatch.Operation to fdiff.Operation.
func mapDiffOp(op diffmatchpatch.Operation) fdiff.Operation {
	switch op {
	case diffmatchpatch.DiffInsert:
		return fdiff.Add
	case diffmatchpatch.DiffDelete:
		return fdiff.Delete
	default:
		return fdiff.Equal
	}
}

// isBinaryContent sniffs the first 8KB of content for null bytes.
func isBinaryContent(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	return bytes.Contains(data[:n], []byte{0})
}

// ---------------------------------------------------------------------------
// Custom types implementing fdiff interfaces
// ---------------------------------------------------------------------------

// patch implements fdiff.Patch.
type patch struct {
	filePatches []fdiff.FilePatch
}

func (p *patch) FilePatches() []fdiff.FilePatch { return p.filePatches }
func (p *patch) Message() string                { return "" }

// filePatch implements fdiff.FilePatch.
type filePatch struct {
	from     fdiff.File
	to       fdiff.File
	chunks   []fdiff.Chunk
	isBinary bool
}

func (fp *filePatch) IsBinary() bool           { return fp.isBinary }
func (fp *filePatch) Files() (fdiff.File, fdiff.File) { return fp.from, fp.to }
func (fp *filePatch) Chunks() []fdiff.Chunk    { return fp.chunks }

// file implements fdiff.File.
type file struct {
	hash plumbing.Hash
	mode filemode.FileMode
	path string
}

func (f *file) Hash() plumbing.Hash    { return f.hash }
func (f *file) Mode() filemode.FileMode { return f.mode }
func (f *file) Path() string           { return f.path }

// chunk implements fdiff.Chunk.
type chunk struct {
	content string
	op      fdiff.Operation
}

func (c *chunk) Content() string    { return c.content }
func (c *chunk) Type() fdiff.Operation { return c.op }
