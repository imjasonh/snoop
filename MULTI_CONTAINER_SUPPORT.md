# Multi-Container Pod Support - Implementation Plan

## Problem Statement

The current snoop implementation has incomplete multi-container pod support:

1. **Auto-discovery exists but isn't used** - `pkg/cgroup/multi_container.go` has `DiscoverPodContainers()` but it's never called
2. **No per-container attribution** - Events contain `cgroup_id` but it's discarded after deduplication
3. **Flat reporting** - Report has a single `Files []string` with no way to know which container accessed which file
4. **Manual cgroup specification** - Users must manually discover and pass cgroup paths

For multi-container pods to be useful, we need to know **which container accessed which files**.

## Goals

1. **Automatic discovery** - Snoop auto-discovers all containers in the pod and excludes itself
2. **Per-container tracking** - Maintain separate file lists for each container
3. **Per-container reporting** - Report shows files grouped by container
4. **Container identification** - Map cgroup IDs to human-readable container names/IDs
5. **Backward compatibility** - Single-container deployments continue to work without changes

## Design

### 1. Configuration Changes

**Remove manual cgroup specification** - No more `-cgroup` or `-cgroups` flags. Auto-discovery is the only mode.

**pkg/config/config.go:**
```go
type Config struct {
    // Remove:
    // CgroupPath string  // DELETED

    // Keep all other fields unchanged
    ReportPath     string
    ReportInterval time.Duration
    ExcludePaths   []string
    ImageRef       string
    // ... etc
}
```

### 2. Container Discovery

**pkg/cgroup/discovery.go:**

Add new function:
```go
// DiscoverAllExceptSelf finds all containers in the current pod,
// excluding snoop's own container.
// Returns a map of cgroup_id -> ContainerInfo
func DiscoverAllExceptSelf() (map[uint64]*ContainerInfo, error)

type ContainerInfo struct {
    CgroupID   uint64
    CgroupPath string
    Name       string  // Short container ID or name
}
```

Implementation:
1. Read `/proc/self/cgroup` to find snoop's cgroup path
2. Get parent directory (pod cgroup)
3. List all subdirectories (containers in the pod)
4. For each container directory:
   - Get cgroup ID via `GetCgroupIDByPath()`
   - Extract container name from path (e.g., `cri-containerd-abc123.scope` → `abc123`)
   - Skip if it matches snoop's own cgroup
5. Return map of cgroup_id → ContainerInfo

### 3. Processor Changes

**pkg/processor/processor.go:**

Current state: Global deduplication, no container tracking.

New state: Per-container deduplication and tracking.

```go
type Processor struct {
    ctx          context.Context
    containers   map[uint64]*containerState  // cgroup_id -> state
    containersMu sync.RWMutex
    excluded     []string
}

type containerState struct {
    info         *cgroup.ContainerInfo
    seen         *lruCache  // Per-container dedup cache
    seenMu       sync.RWMutex
    
    // Per-container metrics
    eventsReceived  uint64
    eventsProcessed uint64
    eventsExcluded  uint64
    eventsDuplicate uint64
    eventsEvicted   uint64
}

// NewProcessor now takes a container map
func NewProcessor(
    ctx context.Context,
    containers map[uint64]*cgroup.ContainerInfo,
    excludePrefixes []string,
    maxUniqueFilesPerContainer int,
) *Processor

// Process now returns which container it was for
func (p *Processor) Process(event *Event) (containerID uint64, path string, result ProcessResult)

// Files now returns per-container file lists
func (p *Processor) Files() map[uint64][]string

// Stats now returns per-container stats
func (p *Processor) Stats() map[uint64]Stats
```

Key changes:
- Track `containerState` per cgroup ID
- Each container has its own LRU cache for deduplication
- Events from unknown cgroups are logged and dropped (shouldn't happen)
- `Files()` returns a map of cgroup_id → file list

### 4. Reporter Changes

**pkg/reporter/reporter.go:**

Current report format:
```json
{
  "container_id": "...",
  "files": ["..."],
  "total_events": 123
}
```

New report format:
```json
{
  "pod_name": "my-app-xyz",
  "namespace": "default",
  "started_at": "2026-01-15T10:00:00Z",
  "last_updated_at": "2026-01-15T10:05:00Z",
  "containers": [
    {
      "name": "nginx",
      "cgroup_id": "12345",
      "cgroup_path": "/kubepods/burstable/pod.../cri-containerd-abc123.scope",
      "files": [
        "/etc/nginx/nginx.conf",
        "/usr/share/nginx/html/index.html"
      ],
      "total_events": 150,
      "unique_files": 2
    },
    {
      "name": "sidecar",
      "cgroup_id": "67890",
      "cgroup_path": "/kubepods/burstable/pod.../cri-containerd-def456.scope",
      "files": [
        "/etc/fluent/fluent.conf"
      ],
      "total_events": 45,
      "unique_files": 1
    }
  ],
  "total_events": 195,
  "dropped_events": 0
}
```

New structs:
```go
type Report struct {
    // Pod-level metadata
    PodName       string    `json:"pod_name,omitempty"`
    Namespace     string    `json:"namespace,omitempty"`
    StartedAt     time.Time `json:"started_at"`
    LastUpdatedAt time.Time `json:"last_updated_at"`
    
    // Per-container data
    Containers []ContainerReport `json:"containers"`
    
    // Aggregate stats
    TotalEvents   uint64 `json:"total_events"`
    DroppedEvents uint64 `json:"dropped_events"`
}

type ContainerReport struct {
    Name        string   `json:"name"`
    CgroupID    uint64   `json:"cgroup_id"`
    CgroupPath  string   `json:"cgroup_path"`
    Files       []string `json:"files"`
    TotalEvents uint64   `json:"total_events"`
    UniqueFiles int      `json:"unique_files"`
}
```

### 5. Main Entry Point Changes

**cmd/snoop/main.go:**

Current flow:
1. Parse flags (including `-cgroup`)
2. Auto-discover if `-cgroup` not set
3. Add single cgroup to probe
4. Create processor with global dedup
5. Process events, discard cgroup_id

New flow:
1. Parse flags (no cgroup flags)
2. Auto-discover all containers except self → `map[uint64]*ContainerInfo`
3. Log discovered containers
4. Add all container cgroup IDs to probe
5. Create processor with container map
6. Process events, track per-container
7. Generate per-container reports

Remove:
```go
// DELETE these flags
flag.StringVar(&cgroupPath, "cgroup", "", "...")
```

Add:
```go
// Auto-discover containers
log.Info("Discovering containers in pod")
containers, err := cgroup.DiscoverAllExceptSelf()
if err != nil {
    return fmt.Errorf("discovering containers: %w", err)
}

if len(containers) == 0 {
    return fmt.Errorf("no containers discovered (pod has only snoop?)")
}

log.Infof("Discovered %d containers to trace", len(containers))
for cgroupID, info := range containers {
    log.Infof("  - %s (cgroup_id=%d, path=%s)", info.Name, cgroupID, info.CgroupPath)
    if err := probe.AddTracedCgroup(cgroupID); err != nil {
        return fmt.Errorf("adding cgroup %s: %w", info.Name, err)
    }
}

// Create processor with container map
proc := processor.NewProcessor(ctx, containers, cfg.ExcludePaths, cfg.MaxUniqueFiles)
```

### 6. Metrics Changes

**pkg/metrics/metrics.go:**

Add per-container metrics with labels:

```go
// Events now have a container label
EventsReceived = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "snoop_events_total",
        Help: "Total events received by syscall and container",
    },
    []string{"syscall", "container"},
)

UniqueFiles = prometheus.NewGaugeVec(
    prometheus.GaugeOpts{
        Name: "snoop_unique_files",
        Help: "Number of unique files per container",
    },
    []string{"container"},
)
```

### 7. Health Check Changes

**pkg/health/health.go:**

Add container-level health:
- Track last event time per container
- Warn if a container hasn't sent events in a while (might indicate it stopped)

## Implementation Steps

### Phase 1: Core Container Discovery ✓
1. ✅ Implement `DiscoverAllExceptSelf()` in `pkg/cgroup/discovery.go`
2. ✅ Add `ContainerInfo` struct
3. ✅ Write unit tests for discovery logic
4. ✅ Test edge cases: single container, multiple containers, snoop-only pod

### Phase 2: Processor Refactor ✓
1. ✅ Add `containers` map to `Processor`
2. ✅ Implement per-container `containerState` with separate LRU caches
3. ✅ Update `Process()` to track container ID
4. ✅ Update `Files()` to return per-container map
5. ✅ Update `Stats()` to return per-container stats
6. ✅ Write unit tests for per-container tracking
7. ✅ Test deduplication works independently per container

### Phase 3: Reporter Refactor ✓
1. ✅ Define new `Report` and `ContainerReport` structs
2. ✅ Update `FileReporter.Update()` to handle new format
3. ✅ Ensure files are sorted within each container
4. ✅ Write unit tests for new report format
5. ✅ Test JSON marshaling/unmarshaling

### Phase 4: Main Integration ✓
1. ✅ Remove `-cgroup` and `-cgroups` flags
2. ✅ Add auto-discovery call
3. ✅ Update processor initialization with container map
4. ✅ Update report generation to use per-container data
5. ✅ Update logging to show per-container info

### Phase 5: Metrics & Health ✓
1. ✅ Add container labels to metrics
2. ✅ Update health checks for per-container tracking
3. ✅ Test Prometheus endpoint shows per-container metrics

### Phase 6: Documentation ✓
1. ✅ Update `deploy/kubernetes/deployment.yaml` - remove cgroup discovery init container
2. ✅ Update `deploy/kubernetes/example-app.yaml` - remove manual cgroup setup
3. ✅ Update `deploy/kubernetes/README.md` - document new behavior
4. ✅ Update `CLAUDE.md` - reflect auto-discovery only
5. ✅ Update `plan.md` - mark milestone properly complete

## Testing Strategy

### Unit Tests

**pkg/cgroup/discovery_test.go:**
- `TestDiscoverAllExceptSelf` - verify self-exclusion works
- `TestDiscoverAllExceptSelf_SingleContainer` - snoop is only container (should error)
- `TestDiscoverAllExceptSelf_MultiContainer` - 3+ containers in pod
- `TestContainerNameExtraction` - various cgroup path formats

**pkg/processor/processor_test.go:**
- `TestPerContainerDeduplication` - same file accessed by 2 containers = 2 entries
- `TestPerContainerStats` - stats tracked independently
- `TestUnknownCgroup` - events from unknown cgroup handled gracefully
- `TestPerContainerLRUEviction` - each container has independent LRU cache

**pkg/reporter/reporter_test.go:**
- `TestNewReportFormat` - validate JSON structure
- `TestMultiContainerReport` - multiple containers with different files
- `TestEmptyContainerReport` - container with no files
- `TestReportSorting` - files sorted within each container

### Integration Tests (KinD)

**Test Setup:**
Create a test pod with 3 containers:
1. `nginx` - web server (accesses nginx config files)
2. `busybox` - sidecar (accesses /etc/hosts, sleeps)
3. `snoop` - monitoring sidecar

**Test 1: Basic Multi-Container Tracing**
```bash
# Deploy test pod
kubectl apply -f test/kind/multi-container-test.yaml

# Wait for pod to be running
kubectl wait --for=condition=Ready pod/multi-container-test

# Generate some file access in each container
kubectl exec multi-container-test -c nginx -- cat /etc/nginx/nginx.conf
kubectl exec multi-container-test -c busybox -- cat /etc/hosts

# Wait for report interval (30s)
sleep 35

# Retrieve report
kubectl cp multi-container-test:/data/snoop-report.json ./report.json -c snoop

# Validate report structure
test/validate-report.sh report.json
```

**Validation checks:**
- ✅ Report has 2 containers (nginx and busybox, not snoop)
- ✅ `nginx` container has `/etc/nginx/nginx.conf` in files list
- ✅ `busybox` container has `/etc/hosts` in files list
- ✅ Files don't leak between containers
- ✅ Container names/IDs are present
- ✅ Cgroup IDs are non-zero
- ✅ Total events > 0 for each container

**Test 2: Container Attribution**
```bash
# Deploy pod
kubectl apply -f test/kind/attribution-test.yaml

# Access same file from different containers
kubectl exec attribution-test -c container1 -- cat /etc/passwd
kubectl exec attribution-test -c container2 -- cat /etc/passwd

# Check report
kubectl cp attribution-test:/data/snoop-report.json ./report.json -c snoop

# Validate: /etc/passwd appears in BOTH container reports
jq '.containers[] | select(.files[] | contains("/etc/passwd")) | .name' report.json
# Should output:
# "container1"
# "container2"
```

**Test 3: Snoop Self-Exclusion**
```bash
# Deploy pod
kubectl apply -f test/kind/self-exclusion-test.yaml

# Trigger file access in snoop container itself
kubectl exec self-exclusion-test -c snoop -- cat /etc/hosts

# Check report
kubectl cp self-exclusion-test:/data/snoop-report.json ./report.json -c snoop

# Validate: snoop's file access is NOT in the report
jq '.containers[].name' report.json
# Should NOT include "snoop"
```

**Test 4: Dynamic Container Discovery**
```bash
# Deploy pod with init containers
kubectl apply -f test/kind/init-container-test.yaml

# Wait for init containers to complete and main containers to start
kubectl wait --for=condition=Ready pod/init-container-test

# Check report - should only show main containers, not init containers
kubectl cp init-container-test:/data/snoop-report.json ./report.json -c snoop
jq '.containers[].name' report.json
# Should show main containers, NOT init containers
```

**Test 5: Metrics Endpoint**
```bash
# Deploy pod
kubectl apply -f test/kind/metrics-test.yaml

# Port-forward to metrics endpoint
kubectl port-forward metrics-test 9090:9090 &

# Generate file access
kubectl exec metrics-test -c app -- cat /etc/passwd

sleep 5

# Check metrics show per-container data
curl -s http://localhost:9090/metrics | grep snoop_events_total
# Should show lines like:
# snoop_events_total{syscall="openat",container="app"} 5
```

**Test 6: Single Container Behavior**
```bash
# Deploy pod with ONLY app + snoop (2 containers total)
kubectl apply -f test/kind/single-app-test.yaml

# This should work fine - discovers 1 app container
kubectl logs single-app-test -c snoop | grep "Discovered"
# Should output: "Discovered 1 container to trace"
```

**Test 7: Error Handling - Snoop Only Pod**
```bash
# Deploy pod with ONLY snoop (1 container total)
kubectl apply -f test/kind/snoop-only-test.yaml

# This should fail gracefully
kubectl logs snoop-only-test -c snoop
# Should output error: "no containers discovered (pod has only snoop?)"
```

### Test Automation

Create `test/kind/run-integration-tests.sh`:
```bash
#!/bin/bash
set -e

# Create KinD cluster
kind create cluster --name snoop-test

# Build and load snoop image
ko build --local --push=false ./cmd/snoop
kind load docker-image --name snoop-test <image>

# Run all integration tests
for test in test/kind/test-*.yaml; do
    echo "Running $(basename $test)..."
    ./test/kind/run-test.sh "$test"
done

# Cleanup
kind delete cluster --name snoop-test
```

## Backward Compatibility

### Breaking Changes
- ❌ `-cgroup` flag removed (was optional, rarely used)
- ❌ `-cgroups` flag removed (never implemented)
- ❌ Report JSON format changed (was: single file list, now: per-container lists)

### Migration Guide

**For single-container deployments:**
No changes needed. Just remove the `-cgroup` flag if you were using it. Report format changes but tools can handle both.

**For multi-container deployments:**
Remove any manual cgroup discovery (init containers, shell wrappers). Snoop now handles this automatically.

**For report consumers:**
Update JSON parsing to handle new format:

Old:
```json
{"files": ["a", "b"]}
```

New:
```json
{"containers": [{"name": "app", "files": ["a", "b"]}]}
```

## Success Criteria

1. ✅ Snoop automatically discovers all containers in a pod
2. ✅ Snoop excludes itself from tracing
3. ✅ Report shows per-container file lists
4. ✅ Same file accessed by 2 containers appears in both lists
5. ✅ Metrics show per-container event counts
6. ✅ All unit tests pass
7. ✅ All KinD integration tests pass
8. ✅ Documentation updated
9. ✅ Example manifests simplified (no manual discovery)

## Timeline

- **Phase 1-2:** Core discovery and processor (~2-3 hours)
- **Phase 3-4:** Reporter and main integration (~1-2 hours)
- **Phase 5:** Metrics and health (~1 hour)
- **Phase 6:** Documentation (~30 min)
- **Testing:** KinD integration tests (~2-3 hours)

**Total estimate:** 1 day of focused work

## Future Enhancements

After this is complete and stable:
- Dynamic container discovery (watch for containers starting/stopping)
- Container name resolution via Kubernetes API (use pod spec names instead of cgroup IDs)
- Per-container resource metrics (CPU/memory impact of tracing)
- Filtering by container name patterns (e.g., only trace "app-*" containers)
