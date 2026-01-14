# Testing Snoop

Since Snoop uses eBPF, it requires a Linux system for testing. This document describes how to test the proof of concept.

## Prerequisites

- Linux system with kernel 5.4+
- Docker or Podman
- Root/sudo access
- bpftool (for generating vmlinux.h)

## Setup on Linux

1. **Generate vmlinux.h** (required once):
   ```bash
   make vmlinux
   ```
   
   This extracts kernel type information needed for eBPF compilation.

2. **Generate eBPF code**:
   ```bash
   make generate
   ```
   
   This compiles the C eBPF program and generates Go bindings.

3. **Build snoop**:
   ```bash
   make build
   ```

## Testing with Docker

### Test 1: Basic File Access Tracing

1. **Start a test container**:
   ```bash
   docker run -d --name test-app alpine:latest sh -c \
     "while true; do cat /etc/passwd > /dev/null; sleep 2; done"
   ```

2. **Find the container's cgroup**:
   ```bash
   ./scripts/find-cgroup.sh test-app
   ```
   
   This will output something like:
   ```
   Container: test-app
   Container ID: abc123...
   Cgroup Path: /system.slice/docker-abc123.scope
   
   To trace this container, run snoop with:
     snoop -cgroup '/system.slice/docker-abc123.scope'
   ```

3. **Run snoop** (requires root):
   ```bash
   sudo ./snoop -cgroup '/system.slice/docker-abc123.scope'
   ```

4. **Expected output**:
   ```
   Loading eBPF program...
   eBPF program loaded successfully
   Tracing cgroup: /system.slice/docker-abc123.scope (ID: 12345)
   Waiting for events (press Ctrl+C to exit)...
   [PID 1234] [Cgroup 12345] [Syscall 257] /etc/passwd
   [PID 1234] [Cgroup 12345] [Syscall 257] /bin/sh
   [PID 1234] [Cgroup 12345] [Syscall 257] /bin/cat
   ```

5. **Verify cgroup filtering works**:
   - Snoop's own file accesses should NOT appear in the output
   - Only the test container's accesses should be shown

6. **Cleanup**:
   ```bash
   docker stop test-app
   docker rm test-app
   ```

### Test 2: Multiple Syscalls

1. **Start a more active container**:
   ```bash
   docker run -d --name test-busy alpine:latest sh -c \
     "while true; do \
       ls /usr/bin > /dev/null; \
       cat /etc/os-release > /dev/null; \
       /bin/sh -c 'echo test' > /dev/null; \
       sleep 1; \
     done"
   ```

2. **Trace it**:
   ```bash
   CGROUP=$(./scripts/find-cgroup.sh test-busy | grep "Cgroup Path:" | cut -d: -f2 | xargs)
   sudo ./snoop -cgroup "$CGROUP"
   ```

3. **Expected output**:
   - Should see both `openat` (syscall 257) and `execve` (syscall 59) events
   - Paths like `/usr/bin/ls`, `/bin/sh`, `/etc/os-release`

4. **Cleanup**:
   ```bash
   docker stop test-busy
   docker rm test-busy
   ```

### Test 3: Docker Compose Test Environment

1. **Start the test environment**:
   ```bash
   cd deploy
   docker compose up -d app
   ```

2. **Get the cgroup**:
   ```bash
   CGROUP=$(../scripts/find-cgroup.sh deploy-app-1 | grep "Cgroup Path:" | cut -d: -f2 | xargs)
   echo "Cgroup: $CGROUP"
   ```

3. **Run snoop**:
   ```bash
   cd ..
   sudo ./snoop -cgroup "$CGROUP"
   ```

4. **Stop the test environment**:
   ```bash
   cd deploy
   docker compose down
   ```

## Troubleshooting

### "failed to load eBPF program"
- Ensure you have `CAP_SYS_ADMIN` capability (run with sudo)
- Verify kernel version: `uname -r` (need 5.4+)
- Check if BTF is available: `ls /sys/kernel/btf/vmlinux`

### "cgroup v2 not found"
- Verify cgroup v2 is enabled: `mount | grep cgroup2`
- If not mounted: `sudo mount -t cgroup2 none /sys/fs/cgroup`

### "No events appearing"
- Verify the container is actually running: `docker ps`
- Check if the cgroup path is correct: `cat /proc/<container-pid>/cgroup`
- Ensure the container is doing file operations

### "cannot find vmlinux.h"
- Run `make vmlinux` to generate it
- If bpftool is not available: `sudo apt-get install linux-tools-generic`

## Success Criteria

For Milestone 1 to be complete, the following must work:

1. ✅ Snoop loads without errors
2. ✅ File access events are captured from target container
3. ✅ Cgroup filtering works (snoop's own accesses don't appear)
4. ✅ Both `openat` and `execve` syscalls are traced
5. ✅ Graceful shutdown on Ctrl+C
6. ✅ No kernel panics or system instability

## Next Steps

After Milestone 1 testing is complete:
- Add more syscalls (stat, access, readlink)
- Implement path normalization
- Add deduplication
- Create JSON report output
- Add Prometheus metrics
