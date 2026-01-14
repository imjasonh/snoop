# Milestone 1 Completion Checklist

## Code Implementation ✅

- [x] eBPF C program (`pkg/ebpf/bpf/snoop.c`)
  - [x] Tracepoint for `sys_enter_openat`
  - [x] Tracepoint for `sys_enter_execve`
  - [x] Ring buffer for events (256KB)
  - [x] Per-CPU heap for event building
  - [x] Traced cgroups hash map
  - [x] Cgroup filtering logic
  - [x] Event structure (cgroup ID, PID, syscall, path)

- [x] Go eBPF loader (`pkg/ebpf/probe.go`)
  - [x] Load eBPF objects with cilium/ebpf
  - [x] Attach tracepoints
  - [x] Manage traced cgroups
  - [x] Ring buffer reader
  - [x] Event parsing
  - [x] Cleanup on close

- [x] Cgroup utilities (`pkg/cgroup/discovery.go`)
  - [x] Get self cgroup ID
  - [x] Get cgroup ID by path
  - [x] Discovery interface
  - [x] Self-excluding discovery stub

- [x] Main application (`cmd/snoop/main.go`)
  - [x] CLI flag parsing
  - [x] Signal handling (SIGINT, SIGTERM)
  - [x] Probe lifecycle management
  - [x] Event display loop
  - [x] Graceful shutdown

## Build Infrastructure ✅

- [x] Go module (`go.mod`)
  - [x] cilium/ebpf dependency
  - [x] golang.org/x/sys dependency

- [x] Dockerfile
  - [x] Multi-stage build
  - [x] clang, llvm, libbpf-dev
  - [x] eBPF code generation
  - [x] Go build
  - [x] Minimal runtime image

- [x] Makefile
  - [x] `vmlinux` target (generate vmlinux.h)
  - [x] `generate` target (eBPF code gen)
  - [x] `build` target
  - [x] `test` target
  - [x] `docker-build` target
  - [x] `clean` target
  - [x] Help text

- [x] ko configuration (`.ko.yaml`)
  - [x] Build settings
  - [x] Base image

- [x] `.gitignore`
  - [x] Binary artifacts
  - [x] Generated eBPF files
  - [x] IDE files

## Testing Infrastructure ✅

- [x] Docker Compose (`deploy/docker-compose.yaml`)
  - [x] Test app service
  - [x] Snoop sidecar configuration
  - [x] Volume mounts for cgroup/tracefs
  - [x] Privileged mode

- [x] Helper scripts
  - [x] `scripts/find-cgroup.sh` - Find container cgroups
  - [x] Executable permissions

- [x] Unit tests
  - [x] `pkg/cgroup/discovery_test.go`

- [x] CI/CD (`.github/workflows/build.yaml`)
  - [x] Ubuntu runner
  - [x] Go setup
  - [x] Dependency installation
  - [x] vmlinux.h generation
  - [x] eBPF code generation
  - [x] Build step
  - [x] Test step
  - [x] go vet
  - [x] staticcheck

## Documentation ✅

- [x] `README.md`
  - [x] Project overview
  - [x] Current status
  - [x] Requirements
  - [x] Build instructions
  - [x] Testing overview
  - [x] Project structure

- [x] `QUICKSTART.md`
  - [x] Prerequisites
  - [x] Generate kernel headers
  - [x] Build steps
  - [x] Run example
  - [x] Docker build option

- [x] `TESTING.md`
  - [x] Prerequisites
  - [x] Setup instructions
  - [x] Test 1: Basic tracing
  - [x] Test 2: Multiple syscalls
  - [x] Test 3: Docker Compose
  - [x] Troubleshooting section
  - [x] Success criteria

- [x] `STATUS.md`
  - [x] What's been done
  - [x] What's left
  - [x] Project structure
  - [x] Design decisions
  - [x] Known limitations
  - [x] Next milestone preview

- [x] `CHECKLIST.md` (this file)

- [x] `plan.md` updates
  - [x] Current status banner
  - [x] Milestone 1 completion status
  - [x] Files created list
  - [x] Current status description

## What's NOT Done (Expected) ⏳

These are intentionally deferred to Milestone 2:

- [ ] Additional syscalls (stat, access, readlink)
- [ ] Path normalization logic
- [ ] Deduplication data structures
- [ ] JSON report output
- [ ] Prometheus metrics
- [ ] Path exclusion filtering
- [ ] Configuration via environment variables

## Testing Required (Needs Linux) ⚠️

Cannot be completed on macOS, requires Linux system:

- [ ] Generate vmlinux.h from kernel
- [ ] Generate eBPF Go bindings
- [ ] Build snoop binary
- [ ] Load eBPF program (verify no errors)
- [ ] Trace test container
- [ ] Verify events captured
- [ ] Verify cgroup filtering works
- [ ] Test graceful shutdown
- [ ] Verify no kernel panics

## How to Mark Milestone 1 Complete

Run the following on a Linux system (kernel 5.4+):

```bash
# 1. Setup
make vmlinux
make generate
make build

# 2. Run test
docker run -d --name test alpine sh -c "while true; do cat /etc/passwd > /dev/null; sleep 2; done"
CGROUP=$(./scripts/find-cgroup.sh test | grep "Cgroup Path:" | cut -d: -f2 | xargs)
sudo ./snoop -cgroup "$CGROUP"

# 3. Expected output
# Should see:
# [PID XXXX] [Cgroup YYYY] [Syscall 257] /etc/passwd
# (Repeating every 2 seconds)

# 4. Test filtering
# Snoop's own file accesses should NOT appear
# Only the test container's accesses

# 5. Test shutdown
# Press Ctrl+C - should exit cleanly

# 6. Cleanup
docker stop test
docker rm test
```

If all tests pass, Milestone 1 is complete! ✅

## Statistics

- **Lines of Code**: ~450 lines (Go + C)
- **Files Created**: 20+
- **Documentation**: 5 markdown files
- **Time to Complete**: ~1 session
- **Dependencies**: 2 (cilium/ebpf, golang.org/x/sys)
