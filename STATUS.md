# Current Status

**Date**: 2026-01-14  
**Milestone**: 1 - eBPF Proof of Concept  
**Status**: Infrastructure Complete, Awaiting Linux Testing

## What's Been Done

### Core Infrastructure ✅

1. **eBPF Program** (`pkg/ebpf/bpf/snoop.c`)
   - Traces `openat` and `execve` syscalls via tracepoints
   - Cgroup-based filtering to trace specific containers
   - Ring buffer for efficient event delivery (256KB)
   - Uses BPF CO-RE for portability

2. **Userspace Loader** (`pkg/ebpf/probe.go`)
   - Loads eBPF program using cilium/ebpf library
   - Manages tracepoint attachments
   - Reads events from ring buffer
   - Manages traced cgroup set

3. **Cgroup Discovery** (`pkg/cgroup/discovery.go`)
   - Get cgroup ID from cgroup path
   - Get self cgroup ID (for filtering)
   - Foundation for "trace all but self" mode

4. **Main Application** (`cmd/snoop/main.go`)
   - Command-line interface with `-cgroup` flag
   - Signal handling for graceful shutdown
   - Event printing to stdout

### Build & Development Tools ✅

- **Dockerfile**: Multi-stage build with all eBPF dependencies
- **Makefile**: Convenient targets for build, generate, test
- **Docker Compose**: Test environment with sample app
- **Helper Scripts**: `find-cgroup.sh` to locate container cgroups
- **GitHub Actions**: CI workflow for automated builds
- **.gitignore**: Proper exclusions for generated files

### Documentation ✅

- **README.md**: Project overview and structure
- **QUICKSTART.md**: Get started in 5 minutes
- **TESTING.md**: Comprehensive testing guide
- **plan.md**: Full technical design (updated with status)

## What's Left for Milestone 1

### Testing on Linux System ⏳

The code is complete but needs to be tested on an actual Linux system because:
- eBPF requires Linux kernel
- vmlinux.h needs to be generated from running kernel
- eBPF programs can only be loaded on Linux

**Testing checklist:**
1. Generate vmlinux.h using bpftool
2. Generate eBPF code with `go generate`
3. Build the snoop binary
4. Start test container (Alpine running `cat /etc/passwd`)
5. Run snoop and verify file access events appear
6. Verify cgroup filtering works (snoop's own accesses filtered)
7. Test graceful shutdown (Ctrl+C)

## Project Structure

```
snoop/
├── cmd/snoop/              # Main application
│   └── main.go
├── pkg/
│   ├── ebpf/              # eBPF loader
│   │   ├── probe.go
│   │   └── bpf/
│   │       ├── snoop.c    # eBPF C program
│   │       └── generate.go
│   └── cgroup/            # Cgroup utilities
│       ├── discovery.go
│       └── discovery_test.go
├── deploy/
│   └── docker-compose.yaml
├── scripts/
│   └── find-cgroup.sh
├── .github/workflows/
│   └── build.yaml
├── Dockerfile
├── Makefile
├── .ko.yaml
├── go.mod
├── go.sum
└── Documentation...
```

## Key Design Decisions

1. **Tracepoints over Kprobes**: Using stable syscall tracepoints for kernel version compatibility
2. **Ring Buffer**: Modern BPF ring buffer instead of perf buffer
3. **Cgroup v2**: Targeting cgroup v2 (standard in modern Linux)
4. **CO-RE**: Compile Once - Run Everywhere for portability
5. **cilium/ebpf**: Using established Go library for eBPF management

## Known Limitations

1. **macOS Development**: Cannot generate or test eBPF on macOS (this is expected)
2. **Cgroup v1**: Currently only supports cgroup v2
3. **Limited Syscalls**: Only `openat` and `execve` for now (more will be added in Milestone 2)
4. **No Deduplication**: Events are printed raw (deduplication comes in Milestone 2)
5. **No Reports**: Currently prints to stdout (JSON reports in Milestone 2)

## Next Milestone: Core Functionality

After Milestone 1 testing is complete, Milestone 2 will add:
- More syscalls (stat, access, readlink, etc.)
- Path normalization (resolve `.`, `..`, relative paths)
- Deduplication (track unique files)
- JSON report output (atomic writes)
- Path exclusions (filter /proc, /sys, etc.)
- Graceful shutdown with final report

## Questions or Blockers?

None currently. The main blocker is testing on a Linux system, which is a prerequisite for any eBPF development.

## How to Continue

1. **If you have a Linux system:**
   - Follow [QUICKSTART.md](QUICKSTART.md) to test
   - Report results back

2. **If using remote Linux (cloud VM, etc.):**
   - Copy the repository to the Linux system
   - Install dependencies: `go`, `clang`, `llvm`, `bpftool`
   - Follow [TESTING.md](TESTING.md)

3. **To continue development:**
   - Start Milestone 2 (can be done in parallel with testing)
   - Add path normalization logic
   - Add deduplication data structure
   - Add JSON report writer
