# Server Binaries + GitClone/GitPush + Auth

## Summary

Complete the remote-hands codebase so it builds and runs: create the proto definition for all existing RPCs, add `GitClone`/`GitPush` RPCs using go-git (in-memory credentials), add a bearer token auth interceptor, and create the two binary entry points (`cmd/remotehands` ConnectRPC server and `cmd/remotehands-mcp` MCP client). The `remotehands-mcp` binary adds relay mode support for use behind an HTTP relay (e.g., `overwatch-fly-relay`).

Existing git operations (`gitStatus`, `gitDiff`, `gitCommit`) continue to use `exec.Command("git", ...)` — no migration. Only the new `GitClone`/`GitPush` RPCs use go-git, which is needed for in-memory credential handling (SSH key or HTTPS PAT never written to disk).

Currently the codebase has all the worker logic and the MCP scaffolding, but the proto file, generated code, and main binaries are missing — the repo does not build.

## Goals

- `proto/remotehands/v1/service.proto` — service definition for all 27 existing RPCs; regenerate `gen/` with `buf generate`
- `go build ./...` passes after proto generation
- New RPCs: `GitClone`, `GitPush` (go-git in-memory auth — SSH key or HTTPS PAT, never written to disk)
- `NewServiceWithGitAuth(homeDir, logger, sshKey, httpsToken)` constructor — credentials as go-git `transport.AuthMethod` objects, used only by GitClone/GitPush
- `internal/worker/auth.go` — bearer token ConnectRPC interceptor (unary + streaming variants)
- `cmd/remotehands/main.go` — standalone ConnectRPC server binary (`--listen`, `--socket`, `--home`, `--auth-token-env`)
- `cmd/remotehands-mcp/main.go` — MCP server binary; direct mode + relay mode (`--relay`, `--machine` flags)

## Non-Goals

- Migrating `gitStatus`, `gitDiff`, `gitCommit` from exec.Command to go-git (keeping them as-is)
- Fly.io integration (overwatch-fly-relay, FlyProvider) — in the overwatch repo
- Docker image or container configuration
- CI/CD changes
- Removing any existing `internal/worker/` functionality

## Depends On

*(none)*

## Current Status: Complete

## Key Files

- [interface.md](interface.md) — New constructor, auth interceptor, proto RPCs, remotehands-mcp relay flags
- [implementation.md](implementation.md) — Phased plan
- [tests.md](tests.md) — Test specifications
