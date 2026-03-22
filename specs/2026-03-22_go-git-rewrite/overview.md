# go-git Rewrite: Eliminate git CLI Dependency

## Summary

Rewrite the three remaining git-CLI-backed RPCs (`gitStatus`, `gitDiff`, `gitCommit`) in the worker package to use the go-git library (v5.17.0, already in go.mod). This eliminates the `git` binary as a runtime dependency for status, diff, and commit operations, aligning them with `gitClone` and `gitPush` which already use go-git. The rewrite also introduces an options-based constructor for author configuration, adds proto fields for per-call author override and file-scoped diffs, and splits the monolithic `git.go` into one file per operation.

## Goals

- Completely remove `exec.Command("git", ...)` calls from `gitStatus`, `gitDiff`, and `gitCommit`
- Produce unified diff output (standard `diff --git a/... b/...` format) via go-git's `plumbing/format/diff` encoder
- Support per-call and init-time author configuration for commits; remove `ensureGitConfig` (no more silently writing to `.gitconfig`); fall back to reading existing `.gitconfig` before erroring
- Add `file_path` field to `GitDiffRequest` for single-file diff filtering
- Add `path`, `author_name`, `author_email` fields to `GitCommitRequest`
- Maintain identical error codes and observable behavior for all existing callers
- Delete `worker/git.go` entirely; split into `git_status.go`, `git_diff.go`, `git_commit.go`
- Update `ServiceGitOptions` constructor, `Ops` interface, `DirectOps`, and MCP tool registrations

## Non-Goals

- Similarity-based rename detection (only explicit renames from `Status()` are reported)
- Submodule support
- Merge conflict resolution
- Interactive staging / partial hunks
- Changes to `gitClone` or `gitPush` (already using go-git)
- New RPCs or new MCP tools (only modifying existing ones)

## Current Status

Complete (2026-03-22). Implemented autonomously via /spec:implement-all.

## Key Files

- [interface.md](interface.md) -- Type signatures for all changed interfaces
- [implementation.md](implementation.md) -- Phased implementation plan with checkboxes
- [tests.md](tests.md) -- Test specifications
- [decisions.md](decisions.md) -- Non-obvious architectural decisions
