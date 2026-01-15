# Multi-Container Pod Test

This test validates snoop's ability to automatically discover and trace multiple containers within a single Kubernetes pod, with per-container file attribution.

## What It Tests

1. **Automatic Container Discovery**
   - Snoop discovers all containers in the pod at startup
   - Self-exclusion: Snoop correctly excludes itself from tracing

2. **Per-Container Tracking**
   - Each container gets independent file tracking
   - Files are attributed to the specific container that accessed them

3. **Shared File Detection**
   - Same file accessed by multiple containers appears in both container reports
   - Demonstrates independent per-container deduplication

4. **Report Format**
   - New JSON structure with per-container file lists
   - Pod-level metadata (pod name, namespace)
   - Per-container statistics (events, unique files)

## Test Pod Configuration

The test pod contains 4 containers:

1. **nginx** - Web server
   - Accesses nginx-specific config files (`/etc/nginx/nginx.conf`)
   - Accesses HTML files (`/usr/share/nginx/html/`)

2. **busybox** - Simple container
   - Accesses system files (`/etc/passwd`, `/etc/group`)
   - Lists directories (`/bin`, `/usr/bin`)

3. **alpine** - Another simple container
   - Accesses some shared files (`/etc/passwd`, `/etc/hosts`)
   - Accesses alpine-specific files (`/etc/alpine-release`)

4. **snoop** - Monitoring sidecar
   - Automatically discovers the 3 app containers
   - Excludes itself from tracing
   - Generates per-container reports

## Running the Test

### Quick Test (Standalone)

```bash
# From test/kind directory
./test-multi-container.sh
```

### Full Test Suite

```bash
# Setup (if not already done)
./setup.sh

# Run all tests including multi-container
./run-tests.sh
```

## Expected Results

The test validates:

- ✅ Report has `containers` array with 3 entries (nginx, busybox, alpine)
- ✅ Snoop is NOT in the containers array (self-excluded)
- ✅ Each container has non-zero files and events
- ✅ nginx container has nginx-specific files
- ✅ busybox and alpine both have `/etc/passwd` (shared file)
- ✅ No excluded paths (`/proc/`, `/sys/`, `/dev/`)
- ✅ All paths are absolute and normalized
- ✅ No duplicate files within each container
- ✅ Prometheus metrics available

## Report Structure

Example output:

```json
{
  "pod_name": "multi-container-test-abc123",
  "namespace": "snoop-test",
  "started_at": "2026-01-15T10:00:00Z",
  "last_updated_at": "2026-01-15T10:00:35Z",
  "containers": [
    {
      "name": "nginx",
      "cgroup_id": 12345,
      "cgroup_path": "/kubepods/.../nginx",
      "files": [
        "/etc/nginx/nginx.conf",
        "/usr/share/nginx/html/index.html"
      ],
      "total_events": 150,
      "unique_files": 2
    },
    {
      "name": "busybox",
      "cgroup_id": 67890,
      "cgroup_path": "/kubepods/.../busybox",
      "files": [
        "/bin/sh",
        "/etc/group",
        "/etc/passwd"
      ],
      "total_events": 200,
      "unique_files": 3
    },
    {
      "name": "alpine",
      "cgroup_id": 13579,
      "cgroup_path": "/kubepods/.../alpine",
      "files": [
        "/etc/alpine-release",
        "/etc/hosts",
        "/etc/passwd"
      ],
      "total_events": 180,
      "unique_files": 3
    }
  ],
  "total_events": 530,
  "dropped_events": 0
}
```

## Validation

The test uses `validate-multi-container.go` to verify:

1. Report structure is valid JSON
2. All required fields are present
3. Expected containers are found
4. Snoop is self-excluded
5. Per-container file lists are valid
6. Shared files are correctly attributed to multiple containers
7. No excluded or malformed paths

## Troubleshooting

### No containers discovered

Check snoop logs:
```bash
kubectl -n snoop-test logs <pod-name> -c snoop
```

Should see:
```
Discovering containers in pod
Discovered 3 containers to trace
  - nginx (cgroup_id=..., path=...)
  - busybox (cgroup_id=..., path=...)
  - alpine (cgroup_id=..., path=...)
```

### Containers have no files

Wait longer (containers access files every 10 seconds):
```bash
# Wait 60 seconds and check again
sleep 60
kubectl -n snoop-test exec <pod-name> -c snoop -- cat /data/snoop-report.json
```

### Snoop appears in report

This is a bug - snoop should self-exclude. Check:
```bash
cat results/multi-container/report.json | jq '.containers[].name'
```

Should NOT include "snoop".

## Cleanup

```bash
kubectl delete -f test/kind/manifests/multi-container-test.yaml
```

## Files

- `manifests/multi-container-test.yaml` - Test pod definition
- `test-multi-container.sh` - Standalone test script
- `validate/validate-multi-container.go` - Report validator
- `MULTI_CONTAINER_TEST.md` - This file
