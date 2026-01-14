# KinD Integration Tests for Snoop

This directory contains integration tests for snoop using KinD (Kubernetes in Docker).

## Quick Start

```bash
# 1. Setup KinD cluster and build image
./setup.sh

# 2. Run all tests
./run-tests.sh

# 3. Clean up
./teardown.sh
```

## Prerequisites

- Docker Desktop running
- `kind` installed: `go install sigs.k8s.io/kind@latest`
- `kubectl` installed
- `jq` installed (for JSON processing)
- Go 1.22+ (for building validator)

## Test Infrastructure

### Scripts

- **setup.sh**: Creates KinD cluster, builds snoop image, applies RBAC
- **run-tests.sh**: Runs all test scenarios and validates results
- **teardown.sh**: Deletes KinD cluster and cleans up

### Test Manifests

Located in `manifests/`:

- **alpine-test.yaml**: Simple Alpine container with predictable file access
- **busybox-script.yaml**: Busybox with controlled file access patterns for testing normalization

### Validation Tool

The `validate/` directory contains a Go program that validates report JSON:

- Checks required fields are present
- Validates no excluded paths present (/proc, /sys, /dev)
- Ensures all paths are absolute and normalized
- Checks for duplicates
- Validates timestamps

Build: `cd validate && go build`

Usage: `./validate <report.json>`

## Test Scenarios

### Test 1: Alpine Basic

Tests basic file access tracing with a simple Alpine container.

**Expected files**: `/etc/passwd`, `/etc/hosts`, `/usr/bin/*`, `/lib/*`

**Validates**:
- Basic eBPF attachment works
- Report is generated
- Common files are captured
- Metadata is populated

### Test 2: Busybox Controlled

Tests path normalization and deduplication with controlled file access.

**Test patterns**:
- Absolute paths: `/etc/passwd`
- Relative paths: `./passwd`, `../etc/hosts` (should normalize)
- Multiple accesses to same file (should deduplicate)
- Temp files: `/tmp/test.txt` (should NOT exclude)

**Validates**:
- Path normalization works correctly
- Deduplication works
- Relative paths become absolute
- `/tmp` is not excluded (only /proc, /sys, /dev)

## How Tests Work

1. **Setup Phase**:
   - Create KinD cluster with eBPF mounts
   - Build snoop Docker image
   - Load image into KinD cluster
   - Apply RBAC resources

2. **Test Execution**:
   - Deploy test workload with snoop sidecar
   - Wait for pod to become ready
   - Check health endpoint
   - Wait 35 seconds for report generation
   - Retrieve report JSON from pod
   - Validate report structure and content
   - Save logs for analysis
   - Clean up deployment

3. **Validation**:
   - Parse JSON report
   - Check required fields
   - Verify file list properties
   - Ensure excluded paths not present
   - Check for path normalization
   - Verify no duplicates

## Results

Test results are saved to `results/`:

```
results/
├── alpine-basic-report.json          # Retrieved report
├── alpine-basic-validation.log       # Validation output
├── alpine-basic-snoop.log            # Snoop container logs
├── alpine-basic-app.log              # App container logs
├── busybox-controlled-report.json
├── busybox-controlled-validation.log
└── ...
```

Results are gitignored and not committed.

## Troubleshooting

### Cluster creation fails

Check Docker is running:
```bash
docker ps
```

### Image build fails

The Dockerfile handles eBPF code generation in a Linux container, so this should work on macOS. Check Docker has enough resources (4GB+ memory).

### Pod not becoming ready

Check pod status and events:
```bash
kubectl -n snoop-test get pods
kubectl -n snoop-test describe pod <pod-name>
```

Check snoop logs:
```bash
kubectl -n snoop-test logs <pod-name> -c snoop
```

Common issues:
- eBPF probes failed to attach (check kernel version, BTF support)
- Permission denied (check capabilities: SYS_ADMIN, BPF, PERFMON)
- Missing mounts (/sys/fs/cgroup, /sys/kernel/debug)

### No report generated

Check if snoop is running:
```bash
kubectl -n snoop-test logs <pod-name> -c snoop --tail=50
```

Check if cgroup discovery worked:
```bash
kubectl -n snoop-test exec <pod-name> -c app -- cat /data/cgroup-path
```

Check if file exists:
```bash
kubectl -n snoop-test exec <pod-name> -c app -- ls -la /data/
```

### Validation fails

Check the actual report content:
```bash
cat results/alpine-basic-report.json | jq .
```

Common validation failures:
- Excluded paths present: Check exclusion config
- Relative paths: Check path normalization logic
- Duplicates: Check deduplication logic
- Empty files array: No events captured, check cgroup targeting

## Testing on Real Clusters

These tests are designed for KinD but can be adapted for real clusters:

1. **GKE/EKS**: Update `cluster-config.yaml` for cloud provider specifics
2. **Node image**: Use your actual node image instead of `kindest/node`
3. **Image registry**: Push snoop image to GCR/ECR instead of loading locally
4. **RBAC**: May need additional permissions based on cluster setup

## Next Steps

After KinD tests pass:

1. Test on a real GKE/EKS cluster
2. Test with more complex applications (Python, Node.js)
3. Test with multiple replicas
4. Test pod restarts and failures
5. Load test with high file access rates
6. Soak test for 24+ hours

See `KIND_TESTING_PLAN.md` for the full testing strategy.

## Related Documentation

- [KIND_TESTING_PLAN.md](../../KIND_TESTING_PLAN.md) - Comprehensive testing plan
- [deploy/kubernetes/README.md](../../deploy/kubernetes/README.md) - Kubernetes deployment docs
- [plan.md](../../plan.md) - Overall project plan
