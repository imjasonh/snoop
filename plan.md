# Snoop: Production File Access Observer

## 🚧 Current Status: Milestone 1 - Proof of Concept Complete

**Last Updated**: 2026-01-14

The foundational infrastructure for Snoop has been implemented:
- ✅ eBPF program with `openat` and `execve` tracing
- ✅ Cgroup-based filtering for targeted container monitoring
- ✅ Ring buffer event delivery from kernel to userspace
- ✅ Go userspace loader using cilium/ebpf
- ✅ Build infrastructure (Dockerfile, Makefile, CI)
- ⏳ **Next**: Test on Linux system with Docker containers

See [Milestone 1](#milestone-1-ebpf-proof-of-concept--in-progress) for details.

---

## Overview

Snoop is a lightweight eBPF-based sidecar that observes file access patterns in production containers. It runs alongside your application, records which files are accessed, and reports this data to help inform image slimming decisions.

### Goals

- **Production-ready**: Negligible performance overhead (<1% CPU, minimal memory)
- **Complete coverage**: Catches all file accesses regardless of binary type (Go, Rust, Python, etc.)
- **Long-running**: Designed to run indefinitely, deduplicating data over time
- **Deployment-aware**: Correlates file access with container image versions
- **Conservative**: Biases toward recording more files, not fewer (best-effort is acceptable)

### Non-goals (for now)

- Enforcement or blocking of file access
- Automatic image rebuilding
- Real-time alerting
- Windows or macOS support (Linux eBPF only)

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Kubernetes Pod / Docker Compose                                │
│                                                                 │
│  ┌─────────────────────┐      ┌─────────────────────────────┐   │
│  │  Application        │      │  Snoop Sidecar              │   │
│  │  Container          │      │                             │   │
│  │                     │      │  ┌───────────────────────┐  │   │
│  │  - Runs unchanged   │      │  │  eBPF Probes          │  │   │
│  │  - No awareness of  │      │  │  (kernel space)       │  │   │
│  │    snoop            │      │  │  - tracepoint/syscalls│  │   │
│  │                     │      │  └───────────┬───────────┘  │   │
│  │                     │      │              │              │   │
│  │                     │      │  ┌───────────▼───────────┐  │   │
│  │                     │      │  │  Event Processor      │  │   │
│  │                     │      │  │  (user space)         │  │   │
│  │                     │      │  │  - cgroup filtering   │  │   │
│  │                     │      │  │  - path normalization │  │   │
│  │                     │      │  │  - deduplication      │  │   │
│  │                     │      │  └───────────┬───────────┘  │   │
│  │                     │      │              │              │   │
│  │                     │      │  ┌───────────▼───────────┐  │   │
│  │                     │      │  │  Reporter             │  │   │
│  │                     │      │  │  - JSON file output   │  │   │
│  │                     │      │  │  - (future) REST API  │  │   │
│  │                     │      │  └───────────────────────┘  │   │
│  └─────────────────────┘      └─────────────────────────────┘   │
│                                              │                  │
│                                              ▼                  │
│                                    /data/snoop-report.json      │
│                                    (shared volume)              │
└─────────────────────────────────────────────────────────────────┘
```

---

## Technical Design

### eBPF Program

The eBPF component attaches to syscall tracepoints to observe file access. We use tracepoints rather than kprobes for stability across kernel versions.

#### Syscalls to Trace

| Syscall | Tracepoint | Purpose |
|---------|------------|---------|
| `openat` | `syscalls/sys_enter_openat` | Primary file open |
| `openat2` | `syscalls/sys_enter_openat2` | Extended file open (kernel 5.6+) |
| `execve` | `syscalls/sys_enter_execve` | Binary execution |
| `execveat` | `syscalls/sys_enter_execveat` | Binary execution (fd-relative) |
| `statx` | `syscalls/sys_enter_statx` | Modern stat (kernel 4.11+) |
| `newfstatat` | `syscalls/sys_enter_newfstatat` | stat with dirfd |
| `faccessat` | `syscalls/sys_enter_faccessat` | Access check |
| `faccessat2` | `syscalls/sys_enter_faccessat2` | Access check (kernel 5.8+) |
| `readlinkat` | `syscalls/sys_enter_readlinkat` | Symlink reading |

Note: We trace `sys_enter_*` (entry) not `sys_exit_*` (exit) because we care about what the app tried to access, not whether it succeeded.

#### eBPF Maps

```c
// Ring buffer for sending events to userspace
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);  // 256KB buffer
} events SEC(".maps");

// Per-CPU array for building event data
struct {
    __uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
    __uint(max_entries, 1);
    __type(key, u32);
    __type(value, struct event);
} heap SEC(".maps");

// Hash set of cgroup IDs to trace (populated from userspace)
struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 64);
    __type(key, u64);      // cgroup ID
    __type(value, u8);     // dummy value (presence = traced)
} traced_cgroups SEC(".maps");
```

#### Event Structure

```c
#define MAX_PATH_LEN 256

struct event {
    u64 cgroup_id;
    u32 pid;
    u32 syscall_nr;
    char path[MAX_PATH_LEN];
};
```

### Userspace Components

#### 1. Cgroup Discovery

Responsible for finding which cgroup(s) to trace.

```go
type CgroupDiscovery interface {
    // Discover returns cgroup IDs for containers we should trace
    Discover(ctx context.Context) ([]uint64, error)
    
    // Watch returns a channel that emits when cgroups change
    // (containers start/stop)
    Watch(ctx context.Context) (<-chan struct{}, error)
}
```

Implementations:
- `SelfExcludingDiscovery`: Trace all cgroups in the pod except snoop's own
- `ExplicitDiscovery`: Trace cgroups specified by container ID
- `ContainerdDiscovery`: Query containerd API for container cgroups

#### 2. Event Processor

Receives raw events from eBPF, normalizes paths, deduplicates.

```go
type EventProcessor struct {
    seen      map[string]struct{}  // dedupe set
    seenMu    sync.RWMutex
    
    excluded  []string             // path prefixes to ignore
    
    metrics   *ProcessorMetrics
}

type ProcessorMetrics struct {
    EventsReceived   prometheus.Counter
    EventsProcessed  prometheus.Counter
    EventsDropped    prometheus.Counter
    UniqueFiles      prometheus.Gauge
    ProcessingTime   prometheus.Histogram
}
```

Path normalization:
- Resolve `.` and `..` components
- Convert relative paths to absolute (using `/proc/<pid>/cwd` if needed)
- Do NOT resolve symlinks (we want to know what the app asked for)

Default exclusions:
- `/proc/*`
- `/sys/*`
- `/dev/*`

#### 3. Reporter

Persists the deduplicated file list.

```go
type Report struct {
    // Identity
    ContainerID   string            `json:"container_id"`
    ImageRef      string            `json:"image_ref"`
    ImageDigest   string            `json:"image_digest,omitempty"`
    PodName       string            `json:"pod_name,omitempty"`
    Namespace     string            `json:"namespace,omitempty"`
    Labels        map[string]string `json:"labels,omitempty"`
    
    // Timing
    StartedAt     time.Time         `json:"started_at"`
    LastUpdatedAt time.Time         `json:"last_updated_at"`
    
    // Data
    Files         []string          `json:"files"`
    
    // Stats
    TotalEvents   uint64            `json:"total_events"`
    DroppedEvents uint64            `json:"dropped_events"`
}

type Reporter interface {
    // Update is called periodically with the current state
    Update(ctx context.Context, report *Report) error
    
    // Close flushes any pending data
    Close() error
}
```

Implementations:
- `FileReporter`: Writes JSON to a file (atomic write via temp + rename)
- `APIReporter`: POSTs to a remote endpoint (future)
- `MultiReporter`: Fans out to multiple reporters

#### 4. Metrics Server

Exposes Prometheus metrics for observability.

```go
// Metrics exposed:
// snoop_events_total{syscall="openat"} - Total events by syscall
// snoop_events_dropped_total - Events dropped due to buffer overflow
// snoop_unique_files - Current count of unique files seen
// snoop_report_writes_total - Number of report writes
// snoop_report_write_errors_total - Failed report writes
// snoop_ebpf_map_size - Current size of eBPF maps
// snoop_process_cpu_seconds_total - CPU usage
// snoop_process_resident_memory_bytes - Memory usage
```

### Configuration

```go
type Config struct {
    // Target selection
    TargetContainerID string        `env:"SNOOP_TARGET_CONTAINER_ID"`
    TargetMode        string        `env:"SNOOP_TARGET_MODE" default:"exclude-self"`
    
    // Identity (for reports)
    ImageRef          string        `env:"SNOOP_IMAGE_REF"`
    ImageDigest       string        `env:"SNOOP_IMAGE_DIGEST"`
    PodName           string        `env:"SNOOP_POD_NAME"`
    Namespace         string        `env:"SNOOP_NAMESPACE"`
    
    // Filtering
    ExcludePaths      []string      `env:"SNOOP_EXCLUDE_PATHS" default:"/proc,/sys,/dev"`
    
    // Output
    ReportPath        string        `env:"SNOOP_REPORT_PATH" default:"/data/snoop-report.json"`
    ReportInterval    time.Duration `env:"SNOOP_REPORT_INTERVAL" default:"30s"`
    
    // API (future)
    APIEndpoint       string        `env:"SNOOP_API_ENDPOINT"`
    APIToken          string        `env:"SNOOP_API_TOKEN"`
    
    // Observability
    MetricsAddr       string        `env:"SNOOP_METRICS_ADDR" default:":9090"`
    LogLevel          string        `env:"SNOOP_LOG_LEVEL" default:"info"`
}
```

### Container Requirements

The snoop sidecar requires elevated privileges to load eBPF programs:

```yaml
securityContext:
  privileged: false
  capabilities:
    add:
      - SYS_ADMIN      # Required for bpf() syscall
      - BPF            # Explicit BPF capability (kernel 5.8+)
      - PERFMON        # For perf events (kernel 5.8+)
  readOnlyRootFilesystem: true
```

Volume mounts:
- `/sys/kernel/debug` (read-only) - For tracefs access
- `/sys/fs/cgroup` (read-only) - For cgroup discovery
- `/data` (read-write) - For report output

---

## Milestones

### Milestone 1: eBPF Proof of Concept ✅ IN PROGRESS

**Goal**: Prove we can trace file syscalls and filter by cgroup from a container.

**Deliverables**:
- [x] Basic Go project structure with `cilium/ebpf`
- [x] eBPF program that traces `openat` and `execve` syscalls
- [x] Userspace loader that prints events to stdout
- [x] Dockerfile for building
- [x] Docker Compose file to test locally with a sample app
- [x] Cgroup discovery utilities
- [x] Helper scripts for finding container cgroups
- [ ] vmlinux.h generation (requires Linux system)
- [ ] End-to-end testing on Linux

**Current Status**:
The core infrastructure is complete. The eBPF program (pkg/ebpf/bpf/snoop.c) traces `openat` and `execve` syscalls with cgroup filtering. The userspace Go loader uses cilium/ebpf to load the program and read events from a ring buffer. The main limitation is that eBPF development requires Linux, so the code needs to be tested on a Linux system.

**Files Created**:
- `cmd/snoop/main.go` - Main entry point with signal handling
- `pkg/ebpf/bpf/snoop.c` - eBPF C program with tracepoint attachments
- `pkg/ebpf/probe.go` - Go loader for eBPF programs
- `pkg/cgroup/discovery.go` - Cgroup ID discovery utilities
- `Dockerfile` - Multi-stage build with clang/llvm
- `deploy/docker-compose.yaml` - Test environment setup
- `scripts/find-cgroup.sh` - Helper to find container cgroups
- `Makefile` - Build automation
- `.github/workflows/build.yaml` - CI pipeline

**Testing** (requires Linux):
- Run snoop alongside `alpine` container running `cat /etc/passwd`
- Verify `/etc/passwd` appears in snoop output
- Verify snoop's own file accesses do NOT appear (cgroup filtering works)

**Success criteria**:
- See file access events from target container
- Filter out events from snoop itself
- No kernel panics or container crashes

**Technical risks**:
- BTF (BPF Type Format) availability in container environments → Addressed with CO-RE support
- Cgroup v1 vs v2 differences → Currently targets cgroup v2
- Kernel version compatibility (target: 5.4+) → Uses stable tracepoints

---

### Milestone 2: Core Functionality

**Goal**: Complete syscall coverage, deduplication, and JSON output.

**Deliverables**:
- [ ] All syscalls traced (openat, execve, stat variants, etc.)
- [ ] Path normalization (resolve `.`, `..`, relative paths)
- [ ] Configurable path exclusions
- [ ] In-memory deduplication with efficient data structure
- [ ] Periodic JSON file output (atomic writes)
- [ ] Graceful shutdown (flush on SIGTERM)

**Testing**:
- Unit tests for path normalization
- Unit tests for deduplication logic
- Integration test: run complex app (e.g., Python Flask), verify expected files appear
- Integration test: verify excluded paths don't appear
- Integration test: kill snoop, verify report was written

**Success criteria**:
- All file access methods captured (open, exec, stat, access, readlink)
- Report contains deduplicated, normalized paths
- No duplicate entries in report
- Clean shutdown writes final report

---

### Milestone 3: Production Hardening

**Goal**: Make snoop reliable and observable for production use.

**Deliverables**:
- [ ] Prometheus metrics endpoint
- [ ] Structured logging with levels (clog)
- [ ] Ring buffer overflow handling and metrics
- [ ] Memory-bounded deduplication (LRU or bloom filter for extreme cases)
- [ ] Health check endpoint
- [ ] Configuration validation
- [ ] Resource limit recommendations documented

**Testing**:
- Load test: high-frequency file access (thousands/sec)
- Measure and document CPU/memory overhead
- Test ring buffer overflow behavior
- Soak test: run for 24+ hours, verify stability
- Test with memory limits, verify graceful degradation

**Success criteria**:
- <1% CPU overhead under normal load
- <50MB memory usage with 100K unique files
- Metrics accurately reflect internal state
- No memory leaks over 24 hours
- Graceful handling of resource pressure

---

### Milestone 4: Kubernetes Integration

**Goal**: Easy deployment in Kubernetes with proper metadata enrichment.

**Deliverables**:
- [ ] Kubernetes deployment manifests
- [ ] Helm chart with configurable values
- [ ] Automatic pod/namespace/image metadata via downward API
- [ ] Support for multi-container pods (trace specific container)
- [ ] Documentation for RBAC requirements
- [ ] Example with common workloads (nginx, Python app, Go service)

**Testing**:
- Deploy in kind cluster
- Deploy in real GKE/EKS cluster
- Test pod restart behavior (snoop survives app restart)
- Test snoop restart behavior (resumes tracing)
- Test with various container runtimes (containerd, CRI-O)

**Success criteria**:
- One-line Helm install
- Works with containerd (default for most clusters)
- Metadata correctly populated in reports
- Survives pod/container restarts

---

### Milestone 5: Multi-Deployment Aggregation

**Goal**: Correlate file access across deployments/versions.

**Deliverables**:
- [ ] Report includes image digest and labels
- [ ] Local CLI tool to merge multiple reports
- [ ] Diff tool: show files accessed in v1 but not v2 (and vice versa)
- [ ] Summary statistics (files by directory, access frequency if tracked)

**Testing**:
- Deploy v1 of app, collect report
- Deploy v2 of app, collect report
- Run diff tool, verify sensible output
- Test with significantly different versions

**Success criteria**:
- Can identify files safe to remove (accessed in v1, not in v2, not in v3...)
- Can identify files always accessed (stable dependencies)
- Useful output for manual slimming decisions

---

### Milestone 6: Remote Reporting API (Future)

**Goal**: Centralized collection and analysis of file access data.

**Deliverables**:
- [ ] API server design document
- [ ] API client in snoop sidecar
- [ ] Buffering and retry logic
- [ ] Authentication (API token or service account)
- [ ] Rate limiting and backpressure

**Testing**:
- API server unit and integration tests
- Client retry behavior under network failures
- Load test with many snoop instances reporting

**Success criteria**:
- Reports reliably delivered to central API
- No data loss during transient failures
- Scales to 1000+ snoop instances

---

## Testing Strategy

### Unit Tests

Location: `*_test.go` files alongside implementation

Coverage targets:
- Path normalization: 100% (critical for correctness)
- Configuration parsing: 100%
- Event processing logic: >90%
- Report serialization: >90%

Test patterns:
- Table-driven tests for path normalization edge cases
- Mock eBPF events for processor testing
- Temp files for reporter testing

### Integration Tests

Location: `integration/` directory

Approach: Use `testscript` for end-to-end scenarios

Example test scenarios:

```
# test_basic_tracing.txtar
# Verify basic file access tracing works

exec docker compose up -d
exec sleep 5

# Trigger file access in target container
exec docker compose exec app cat /etc/passwd
exec docker compose exec app ls /usr

# Wait for report
exec sleep 35

# Verify report contents
exec cat /tmp/snoop-report.json
stdout '"files":'
stdout '/etc/passwd'
stdout '/usr'

exec docker compose down
```

### Performance Tests

Location: `bench/` directory

Metrics to measure:
- Events processed per second (target: >100K/sec)
- Latency added to syscalls (target: <1μs p99)
- Memory usage vs unique file count
- CPU usage under load

Benchmark scenarios:
1. **Idle**: No file access, measure baseline overhead
2. **Steady**: 100 file accesses/sec, sustained
3. **Burst**: 10K file accesses in 1 second
4. **Stress**: Maximum sustainable throughput

Tools:
- `pprof` for CPU/memory profiling
- Custom benchmark harness that generates file access patterns
- `perf` for syscall latency measurement

### Compatibility Tests

Test matrix:

| Kernel Version | Cgroup Version | Container Runtime | Status |
|----------------|----------------|-------------------|--------|
| 5.4 (Ubuntu 20.04) | v1 | containerd | Must work |
| 5.10 (Debian 11) | v2 | containerd | Must work |
| 5.15 (Ubuntu 22.04) | v2 | containerd | Must work |
| 6.1 (Debian 12) | v2 | containerd | Must work |
| 5.10 | v2 | CRI-O | Should work |

Testing approach:
- GitHub Actions matrix with different base images
- Manual testing on GKE, EKS, local kind

### Chaos Tests

Scenarios:
- Kill snoop mid-operation, verify no corruption
- Fill disk, verify graceful handling
- OOM kill snoop, verify kernel stability (no leaked eBPF programs)
- Network partition (for future API reporting)

---

## Directory Structure

```
snoop/
├── cmd/
│   └── snoop/
│       └── main.go              # Entry point
├── pkg/
│   ├── ebpf/
│   │   ├── probe.go             # eBPF loader and manager
│   │   ├── probe_test.go
│   │   └── bpf/
│   │       ├── snoop.c          # eBPF C code
│   │       └── snoop.go         # Generated Go bindings
│   ├── cgroup/
│   │   ├── discovery.go         # Cgroup discovery interface
│   │   ├── discovery_test.go
│   │   ├── self_excluding.go    # "Trace all but me" implementation
│   │   └── containerd.go        # Containerd API implementation
│   ├── processor/
│   │   ├── processor.go         # Event processing and dedup
│   │   ├── processor_test.go
│   │   ├── normalize.go         # Path normalization
│   │   └── normalize_test.go
│   ├── reporter/
│   │   ├── reporter.go          # Reporter interface
│   │   ├── file.go              # JSON file reporter
│   │   ├── file_test.go
│   │   ├── api.go               # Future API reporter
│   │   └── multi.go             # Multi-reporter fan-out
│   ├── config/
│   │   ├── config.go            # Configuration struct
│   │   └── config_test.go
│   └── metrics/
│       └── metrics.go           # Prometheus metrics
├── integration/
│   ├── basic_test.go            # Integration tests
│   └── testdata/
│       └── *.txtar              # testscript test cases
├── bench/
│   ├── bench_test.go            # Benchmarks
│   └── generate.go              # File access generator
├── deploy/
│   ├── docker-compose.yaml      # Local development
│   ├── kubernetes/
│   │   ├── deployment.yaml
│   │   ├── rbac.yaml
│   │   └── example-app.yaml
│   └── helm/
│       └── snoop/
│           ├── Chart.yaml
│           ├── values.yaml
│           └── templates/
├── tools/
│   ├── snoop-merge/             # CLI to merge reports
│   │   └── main.go
│   └── snoop-diff/              # CLI to diff reports
│       └── main.go
├── docs/
│   ├── getting-started.md
│   ├── configuration.md
│   ├── troubleshooting.md
│   └── architecture.md
├── .ko.yaml                     # ko build configuration
├── go.mod
├── go.sum
└── plan.md                      # This file
```

---

## Dependencies

### Go Libraries

| Library | Purpose | Version |
|---------|---------|---------|
| `github.com/cilium/ebpf` | eBPF loading and management | v0.12+ |
| `github.com/chainguard-dev/clog` | Structured logging | latest |
| `github.com/sethvargo/go-envconfig` | Configuration parsing | latest |
| `github.com/prometheus/client_golang` | Metrics | v1.17+ |

### Build Tools

| Tool | Purpose |
|------|---------|
| `ko` | Container image building |
| `bpf2go` | eBPF C to Go code generation (part of cilium/ebpf) |
| `clang` | eBPF C compilation |
| `llvm` | eBPF bytecode generation |

### Development Tools

| Tool | Purpose |
|------|---------|
| `docker` / `podman` | Local container testing |
| `kind` | Local Kubernetes testing |
| `helm` | Kubernetes package management |

---

## Risk Assessment

### Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| BTF not available in target environment | Medium | High | Ship with CO-RE (Compile Once, Run Everywhere) or embedded BTF |
| Cgroup v1/v2 differences | Medium | Medium | Test both, abstract behind discovery interface |
| Kernel version incompatibility | Low | High | Target 5.4+ explicitly, test matrix |
| Ring buffer overflow under load | Medium | Low | Metrics, tunable buffer size, documented limits |
| Memory growth with many unique files | Low | Medium | Bounded data structures, bloom filter fallback |

### Operational Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Snoop sidecar increases attack surface | Medium | Medium | Minimal privileges, read-only rootfs, security audit |
| Misconfiguration leads to missing data | Medium | Medium | Validation, sensible defaults, clear documentation |
| Report file fills disk | Low | Medium | Rotation, size limits, monitoring |

---

## Open Questions

Deferred for later decision:

1. **Target container identification**: Explicit ID vs. "all but me" vs. annotation-based
2. **Image metadata source**: Environment variables vs. container runtime API
3. **Path normalization**: How much to normalize? Resolve symlinks?
4. **Temporary files**: Include `/tmp` in reports or exclude?
5. **Report format**: JSON sufficient, or support other formats?
6. **Report granularity**: Per-container, per-pod, per-deployment?

---

## Success Metrics

How we'll know snoop is working:

1. **Correctness**: Reports contain all files accessed by the app (validated by manual inspection)
2. **Performance**: <1% CPU overhead, <50MB memory for typical workloads
3. **Reliability**: No crashes or data loss over extended operation (24+ hours)
4. **Usability**: Clear documentation, easy deployment, actionable output
5. **Adoption**: Successfully used to slim at least one real production image

---

## References

- [cilium/ebpf documentation](https://ebpf-go.dev/)
- [Linux tracepoints](https://www.kernel.org/doc/html/latest/trace/tracepoints.html)
- [BPF ring buffer](https://nakryiko.com/posts/bpf-ringbuf/)
- [Cgroup v2 documentation](https://docs.kernel.org/admin-guide/cgroup-v2.html)
- [ko documentation](https://ko.build/)
- [SlimToolkit](https://github.com/slimtoolkit/slim) (prior art)
- [Tracee](https://github.com/aquasecurity/tracee) (prior art)
- [Tetragon](https://github.com/cilium/tetragon) (prior art)
