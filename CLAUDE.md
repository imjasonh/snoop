# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Important: Git Workflow

**NEVER run `git commit` yourself.** When you complete work, write a brief commit message but let the user commit. This applies even if the user says "commit when done" - always stop and provide the commit message instead of committing.

## What is Snoop?

Snoop is an eBPF-based sidecar that observes file access patterns in production containers. It traces syscalls (openat, execve, stat, access, readlink variants), deduplicates paths, and writes periodic JSON reports. The goal is to inform container image slimming decisions.

## Build Commands

**Note**: eBPF code generation requires Linux with BTF support. On macOS, you can only work with Go code.

```bash
# Generate vmlinux.h (Linux only, requires bpftool)
make vmlinux

# Generate eBPF Go bindings (Linux only, requires clang/llvm)
make generate

# Build binary (requires generated eBPF code)
make build

# Run tests (pkg/ebpf tests will fail without generated code)
go test ./...

# Run tests for a specific package
go test ./pkg/processor/...
go test -v -run TestNormalizePath ./pkg/processor/

# Build Docker image (handles eBPF generation inside container)
make docker-build

# Start/stop test environment
make docker-compose-up
make docker-compose-down
```

## Architecture

```
cmd/snoop/main.go          Entry point, CLI flags, event loop
pkg/ebpf/                  eBPF loader and probe management
  bpf/snoop.c              eBPF C program (tracepoints on syscalls)
  bpf/generate.go          go:generate directive for bpf2go
  probe.go                 Go wrapper for loading/reading eBPF
pkg/cgroup/                Cgroup ID discovery for container targeting
pkg/processor/             Path normalization, exclusions, deduplication
pkg/reporter/              JSON file output with atomic writes
```

**Data flow**: Kernel tracepoints → eBPF ring buffer → Go event reader → Processor (normalize, dedupe) → Reporter (periodic JSON writes)

**Cgroup filtering**: The eBPF program only emits events for cgroups added to the `traced_cgroups` map. This allows targeting specific containers.

## Key Design Decisions

- Traces syscall entry (not exit) because we care about what apps *tried* to access, not success/failure
- Does NOT follow symlinks during normalization - records what the app asked for
- Default exclusions: `/proc/`, `/sys/`, `/dev/`
- Atomic file writes via temp file + rename
- Thread-safe deduplication using sync.RWMutex-protected map

## Testing

`go test ./...` works on any platform. The `pkg/ebpf/` and `cmd/snoop/` packages have `//go:build linux` tags, so they are skipped on macOS. The testable packages (`pkg/processor/`, `pkg/reporter/`, `pkg/cgroup/`) have full coverage and run everywhere.

On Linux with generated eBPF code, all packages are included in the test run.

## Current Status

See `plan.md` for the full roadmap. Milestone 2 (core functionality) is complete. Next is Milestone 3 (production hardening: metrics, logging, health checks).
