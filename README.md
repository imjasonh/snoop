# Snoop

A lightweight eBPF-based sidecar that observes file access patterns in production containers.

## Current Status: Milestone 1 - eBPF Proof of Concept

This is an initial proof of concept demonstrating:
- Basic eBPF program tracing `openat` and `execve` syscalls
- Cgroup-based filtering to trace specific containers
- Userspace Go loader using cilium/ebpf
- Ring buffer for efficient event delivery

## Requirements

### Build Requirements
- Go 1.21+
- clang (for eBPF compilation)
- llvm (for eBPF bytecode generation)
- Linux kernel 5.4+ with BTF support
- Linux headers or vmlinux.h

### Runtime Requirements
- Linux with kernel 5.4+
- Cgroup v2 enabled
- Capabilities: `CAP_SYS_ADMIN`, `CAP_BPF` (kernel 5.8+), `CAP_PERFMON` (kernel 5.8+)

## Building

### Generate eBPF code

First, generate vmlinux.h from your running kernel (on a Linux system):

```bash
bpftool btf dump file /sys/kernel/btf/vmlinux format c > pkg/ebpf/bpf/vmlinux.h
```

Then generate the Go bindings:

```bash
go generate ./pkg/ebpf/bpf
```

### Build the binary

```bash
go build -o snoop ./cmd/snoop
```

### Build with Docker

```bash
docker build -t snoop:latest .
```

## Testing Locally with Docker Compose

1. Start the test application:
```bash
cd deploy
docker compose up -d app
```

2. Find the cgroup path for the app container:
```bash
../scripts/find-cgroup.sh deploy-app-1
```

3. Run snoop to trace the container:
```bash
sudo ./snoop -cgroup <cgroup-path-from-step-2>
```

You should see file access events like:
```
[PID 1234] [Cgroup 567] [Syscall 257] /etc/passwd
[PID 1234] [Cgroup 567] [Syscall 257] /usr/bin/ls
```

## Project Structure

```
snoop/
├── cmd/snoop/          # Main entry point
├── pkg/
│   ├── ebpf/          # eBPF loader and management
│   │   └── bpf/       # eBPF C code
│   └── cgroup/        # Cgroup discovery
├── deploy/            # Deployment configurations
├── scripts/           # Helper scripts
└── plan.md           # Full technical design
```

## Documentation

- [plan.md](plan.md) - Complete technical design and roadmap
- [RESOURCE_LIMITS.md](RESOURCE_LIMITS.md) - Resource limits and production recommendations

## Development Status

See [plan.md](plan.md) for the complete technical design and roadmap.

### Completed
- [x] Basic Go project structure
- [x] eBPF program for openat and execve tracing
- [x] Cgroup-based filtering
- [x] Ring buffer event delivery
- [x] Basic userspace loader

### Next Steps
- [ ] Generate vmlinux.h on Linux system
- [ ] Test on Linux with Docker
- [ ] Add more syscalls (stat, access, readlink)
- [ ] Path normalization
- [ ] Deduplication
- [ ] JSON report output

## Notes

- This project is in early proof-of-concept stage
- eBPF development requires Linux; development on macOS is limited to Go code
- For full functionality, build and test on a Linux system with kernel 5.4+
