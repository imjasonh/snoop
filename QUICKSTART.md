# Quick Start Guide

Get Snoop running in 5 minutes on a Linux system.

## Prerequisites

- Linux with kernel 5.4+
- Docker installed
- Root/sudo access

## 1. Generate Kernel Headers

```bash
# Install bpftool if not present
sudo apt-get install -y linux-tools-$(uname -r) || sudo apt-get install -y linux-tools-generic

# Generate vmlinux.h
sudo bpftool btf dump file /sys/kernel/btf/vmlinux format c > pkg/ebpf/bpf/vmlinux.h
```

## 2. Build

```bash
# Install build dependencies
sudo apt-get install -y clang llvm golang-go

# Generate eBPF code
go generate ./pkg/ebpf/bpf

# Build snoop
go build -o snoop ./cmd/snoop
```

## 3. Run

```bash
# Start a test container
docker run -d --name myapp alpine:latest sh -c "while true; do cat /etc/passwd > /dev/null; sleep 2; done"

# Find its cgroup
./scripts/find-cgroup.sh myapp

# Trace it (replace with your cgroup path)
sudo ./snoop -cgroup '/system.slice/docker-CONTAINERID.scope'
```

You should see output like:
```
[PID 1234] [Cgroup 5678] [Syscall 257] /etc/passwd
```

Press Ctrl+C to stop.

## 4. Cleanup

```bash
docker stop myapp
docker rm myapp
```

## Using Docker Build

If you prefer to build in Docker:

```bash
# Build the image
docker build -t snoop:latest .

# Run it (needs privileged mode for eBPF)
docker run --rm -it --privileged \
  --pid=host \
  -v /sys/fs/cgroup:/sys/fs/cgroup:ro \
  -v /sys/kernel/debug:/sys/kernel/debug:ro \
  snoop:latest -cgroup '/path/to/cgroup'
```

## Next Steps

- See [TESTING.md](TESTING.md) for detailed test scenarios
- See [plan.md](plan.md) for the full design and roadmap
- See [README.md](README.md) for architecture overview
