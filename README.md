![snoop](snoop.png)

**Snoop** is an eBPF-based sidecar that observes file access patterns in production containers to inform image slimming decisions.

It was vibe-coded in like an hour, don't use it in prod. Don't use it at all, see if I care.

## Overview

Snoop runs as a sidecar container alongside your application, using eBPF to trace file system syscalls. It records which files your application accesses and generates JSON reports to help you identify unused files in your container images.

### Key Features

- **Zero application changes**: Traces syscalls in the kernel, no instrumentation needed
- **Low overhead**: <1% CPU, 32-128 MB memory for typical workloads
- **Production ready**: Prometheus metrics, health checks, graceful shutdown
- **Kubernetes native**: Manifests, RBAC, automatic multi-container pod support
- **Per-container attribution**: Tracks which container accessed which files
- **Best-effort design**: Conservative filtering (records more rather than less)

### Non-goals

- Enforcement or blocking of file access
- Automatic image rebuilding
- Real-time alerting
- Windows or macOS support (Linux eBPF only)

## Quick Start

### Docker Compose (Local Testing)

```bash
# Start a test application with snoop sidecar
make docker-compose-up

# View the report after 30 seconds
docker exec deploy-app-1 cat /data/snoop-report.json

# View metrics
curl http://localhost:9090/metrics

# Stop
make docker-compose-down
```

### Kubernetes

```bash
# Apply RBAC resources
kubectl apply -f deploy/kubernetes/rbac.yaml

# Deploy example application with snoop sidecar
kubectl apply -f deploy/kubernetes/example-app.yaml

# Check status
kubectl -n snoop-system get pods

# View logs
kubectl -n snoop-system logs -f deployment/nginx-with-snoop -c snoop

# View report
kubectl -n snoop-system exec deployment/nginx-with-snoop -c nginx -- cat /data/snoop-report.json

# View metrics
kubectl -n snoop-system port-forward deployment/nginx-with-snoop 9090:9090
curl http://localhost:9090/metrics

# Clean up
kubectl delete -f deploy/kubernetes/example-app.yaml
kubectl delete -f deploy/kubernetes/rbac.yaml
```

See [deploy/kubernetes/README.md](deploy/kubernetes/README.md) for detailed Kubernetes deployment instructions.

## Building

### Requirements

Snoop must be built on Linux with:

- Go 1.21+
- Linux kernel 5.4+ with BTF support
- clang and llvm (for eBPF compilation)
- bpftool (for vmlinux.h generation)

On macOS, you can only work with Go code. Use Docker to build:

```bash
make generate-in-docker
```

### Build Steps

#### On Linux

```bash
# 1. Generate vmlinux.h from your kernel
make vmlinux

# 2. Generate eBPF Go bindings
make generate

# 3. Build the binary
make build

# 4. Run tests
make test
```

#### On macOS (using Docker)

```bash
# Generate eBPF code in Docker (works on any platform)
make generate-in-docker

# Build Docker image (includes eBPF generation)
make docker-build

# Run tests (pkg/ebpf tests will be skipped)
go test ./pkg/processor/ ./pkg/reporter/ ./pkg/cgroup/
```

#### Manual Build

```bash
# Generate vmlinux.h (Linux only)
bpftool btf dump file /sys/kernel/btf/vmlinux format c > pkg/ebpf/bpf/vmlinux.h

# Generate eBPF code (Linux only, requires clang/llvm)
go generate ./pkg/ebpf/bpf

# Build binary
go build -o snoop ./cmd/snoop

# Build Docker image
docker build -t snoop:latest .
```

## Deployment

### Adding Snoop to Your Application

To add snoop as a sidecar to an existing Kubernetes deployment:

1. **Add the snoop container** to your pod spec
2. **Add required volumes** (cgroup, debugfs, shared data volume)
3. **Apply RBAC** if not already present

Complete example in [deploy/kubernetes/example-app.yaml](deploy/kubernetes/example-app.yaml).

**Note**: Snoop automatically discovers all containers in the pod at startup and excludes itself. No manual cgroup configuration is required.

### Configuration

Key command-line arguments:

| Flag | Default | Description |
|------|---------|-------------|
| `-report` | `/data/snoop-report.json` | Path to write JSON reports |
| `-interval` | `30s` | Interval between report writes |
| `-exclude` | `/proc/,/sys/,/dev/` | Path prefixes to exclude |
| `-max-unique-files` | `100000` | Max unique files per container (0 = unbounded) |
| `-metrics-addr` | `:9090` | Address for metrics/health endpoint |
| `-log-level` | `info` | Log level (debug, info, warn, error) |

Environment variables can also be used (prefix with `SNOOP_`, e.g., `SNOOP_LOG_LEVEL=debug`).

### Resource Requirements

The snoop sidecar needs elevated capabilities to load eBPF programs:

```yaml
securityContext:
  privileged: false
  capabilities:
    add:
      - SYS_ADMIN  # Required for bpf() syscall
      - BPF        # Explicit BPF capability (kernel 5.8+)
      - PERFMON    # For perf events (kernel 5.8+)
  readOnlyRootFilesystem: true
```

Recommended resource limits:

```yaml
resources:
  requests:
    cpu: 50m
    memory: 64Mi
  limits:
    cpu: 200m
    memory: 128Mi
```

See [RESOURCE_LIMITS.md](RESOURCE_LIMITS.md) for detailed recommendations and tuning guidance.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Kubernetes Pod / Docker Container                          │
│                                                             │
│  ┌──────────────────┐      ┌──────────────────────────┐    │
│  │  Application     │      │  Snoop Sidecar           │    │
│  │  Container       │      │                          │    │
│  │                  │      │  ┌────────────────────┐  │    │
│  │  • Runs         │      │  │  eBPF Probes       │  │    │
│  │    unchanged    │      │  │  (kernel tracing)  │  │    │
│  │  • No awareness │      │  └──────────┬─────────┘  │    │
│  │    of snoop     │      │             │            │    │
│  └──────────────────┘      │  ┌──────────▼─────────┐  │    │
│                            │  │  Event Processor   │  │    │
│                            │  │  • Normalize paths │  │    │
│                            │  │  • Deduplicate     │  │    │
│                            │  └──────────┬─────────┘  │    │
│                            │             │            │    │
│                            │  ┌──────────▼─────────┐  │    │
│                            │  │  JSON Reporter     │  │    │
│                            │  │  • Atomic writes   │  │    │
│                            │  └────────────────────┘  │    │
│                            │                          │    │
│                            │  HTTP :9090              │    │
│                            │  • /metrics (Prometheus) │    │
│                            │  • /healthz             │    │
│                            └──────────────────────────┘    │
│                                         │                  │
│                                         ▼                  │
│                              /data/snoop-report.json       │
│                              (shared volume)               │
└─────────────────────────────────────────────────────────────┘
```

### How It Works

1. **eBPF probes** attach to syscall tracepoints (openat, execve, stat, access, readlink)
2. **Cgroup filtering** ensures only target container events are captured
3. **Event processor** normalizes paths, excludes system paths, and deduplicates
4. **Reporter** writes periodic JSON reports with atomic file operations
5. **Metrics server** exposes Prometheus metrics and health checks

## Output

Snoop generates JSON reports with per-container file attribution:

```json
{
  "pod_name": "my-app-7d4f8b9c5d-x7k9m",
  "namespace": "default",
  "started_at": "2026-01-15T10:30:00Z",
  "last_updated_at": "2026-01-15T10:31:00Z",
  "containers": [
    {
      "name": "nginx",
      "cgroup_id": 12345,
      "cgroup_path": "/kubepods/burstable/pod.../nginx",
      "files": [
        "/etc/nginx/nginx.conf",
        "/etc/nginx/conf.d/default.conf",
        "/usr/share/nginx/html/index.html"
      ],
      "total_events": 1200,
      "unique_files": 3
    },
    {
      "name": "sidecar",
      "cgroup_id": 67890,
      "cgroup_path": "/kubepods/burstable/pod.../sidecar",
      "files": [
        "/etc/fluent/fluent.conf",
        "/var/log/app.log"
      ],
      "total_events": 323,
      "unique_files": 2
    }
  ],
  "total_events": 1523,
  "dropped_events": 0
}
```

**Multi-Container Support**: Each container in the pod gets its own entry with independent file tracking. If multiple containers access the same file, it appears in each container's list.

## Monitoring

Snoop exposes Prometheus metrics on port 9090:

- `snoop_events_total` - Total events by syscall type
- `snoop_events_processed_total` - Events resulting in new files
- `snoop_events_dropped_total` - Events dropped due to buffer overflow
- `snoop_unique_files` - Current count of unique files tracked
- `snoop_report_writes_total` - Number of report writes
- `snoop_report_write_errors_total` - Failed report writes

Health check endpoint: `GET /healthz` (returns 200 OK if healthy)

## Testing

```bash
# Run all tests (Linux with generated eBPF code)
go test ./...

# Run tests for specific packages (works on any platform)
go test ./pkg/processor/
go test ./pkg/reporter/
go test ./pkg/cgroup/

# Run with verbose output
go test -v ./pkg/processor/

# Run specific test
go test -v -run TestNormalizePath ./pkg/processor/
```

## Troubleshooting

### eBPF program fails to load

Check kernel version and BTF support:

```bash
uname -r  # Should be 5.4 or higher
ls -la /sys/kernel/btf/vmlinux  # Should exist
```

### No events are recorded

Verify the cgroup path is correct:

```bash
# In the snoop container
cat /proc/self/cgroup
```

Check snoop logs for errors:

```bash
kubectl logs <pod-name> -c snoop
```

### High memory usage

Set a limit on unique files:

```bash
-max-unique-files=50000  # ~12 MB max
```

Monitor the `snoop_unique_files` metric to track growth.

### Events being dropped

This is expected under extreme load (>10K events/sec). Check metrics:

```bash
curl http://localhost:9090/metrics | grep snoop_events_dropped_total
```

Solutions:
- Increase CPU limits to process events faster
- Accept data loss (snoop is best-effort by design)

See [RESOURCE_LIMITS.md](RESOURCE_LIMITS.md) for detailed troubleshooting.

## Project Structure

```
snoop/
├── cmd/snoop/              # Main entry point
├── pkg/
│   ├── ebpf/              # eBPF loader and probes
│   │   └── bpf/           # eBPF C code and generated Go
│   ├── cgroup/            # Cgroup discovery
│   ├── processor/         # Path normalization and deduplication
│   ├── reporter/          # JSON report output
│   ├── config/            # Configuration management
│   └── metrics/           # Prometheus metrics
├── deploy/
│   ├── docker-compose.yaml     # Local development
│   └── kubernetes/             # K8s manifests
│       ├── rbac.yaml
│       ├── deployment.yaml
│       ├── example-app.yaml
│       └── README.md
├── Dockerfile             # Multi-stage Docker build
├── Makefile              # Build automation
├── plan.md               # Technical design and roadmap
└── RESOURCE_LIMITS.md    # Resource tuning guide
```

## Documentation

- [plan.md](plan.md) - Complete technical design and roadmap
- [RESOURCE_LIMITS.md](RESOURCE_LIMITS.md) - Resource limits and tuning
- [deploy/kubernetes/README.md](deploy/kubernetes/README.md) - Kubernetes deployment guide
- [CLAUDE.md](CLAUDE.md) - Development guide for Claude Code

## Development

### On Linux

```bash
# Generate vmlinux.h from your kernel
make vmlinux

# Generate eBPF Go bindings
make generate

# Build binary
make build

# Run tests
make test

# Start local test environment
make docker-compose-up
```

### On macOS

eBPF code generation requires Linux. Use Docker:

```bash
# Generate eBPF code in Docker (extracts generated files)
make generate-in-docker

# Work with Go code (eBPF tests will be skipped)
go test ./pkg/processor/ ./pkg/reporter/ ./pkg/cgroup/

# Build Docker image
make docker-build
```

## Contributing

This project follows standard Go conventions:

- Use `go fmt` for formatting
- Add tests for new functionality
- Update documentation for user-facing changes
- Follow the patterns in existing code

## License

Apache License 2.0

## Prior Art

- [SlimToolkit](https://github.com/slimtoolkit/slim) - Container image analysis and optimization
- [Tracee](https://github.com/aquasecurity/tracee) - Runtime security with eBPF
- [Tetragon](https://github.com/cilium/tetragon) - Security observability with eBPF

## Roadmap

- ✅ **Milestone 1**: eBPF proof of concept
- ✅ **Milestone 2**: Core functionality (all syscalls, deduplication, reports)
- ✅ **Milestone 3**: Production hardening (metrics, logging, health checks)
- ✅ **Milestone 4**: Kubernetes integration with multi-container support
- 📋 **Milestone 5**: Multi-deployment aggregation (report merging, diff tools)
- 📋 **Milestone 6**: Remote reporting API (centralized collection)

See [plan.md](plan.md) for detailed milestone information.
