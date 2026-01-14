# Multi-Container Pod Support

## Overview

Snoop now supports tracing multiple containers within a single Kubernetes pod. This is useful when you have multi-container pods (e.g., app + sidecar + snoop) and want to trace file access from specific containers while excluding others.

## Implementation

### Command-Line Interface

Added a new `-cgroups` flag that accepts comma-separated cgroup paths:

```bash
# Single container (backwards compatible)
snoop -cgroup=/sys/fs/cgroup/kubepods/pod123/container1

# Multiple containers (new)
snoop -cgroups=/sys/fs/cgroup/kubepods/pod123/container1,/sys/fs/cgroup/kubepods/pod123/container2
```

The old `-cgroup` flag is still supported for backwards compatibility and will be automatically migrated to the new `CgroupPaths` field internally.

### Configuration Changes

**pkg/config/config.go:**
- Added `CgroupPaths []string` field to hold multiple cgroup paths
- Deprecated `CgroupPath string` (but still supported for backwards compatibility)
- Updated `Validate()` to migrate single path to slice
- Added `ParseCgroupPaths()` helper function

**cmd/snoop/main.go:**
- Added `-cgroups` flag for comma-separated paths
- Updated probe initialization to add all specified cgroups
- Logs show which cgroups are being traced (N/M format)

### eBPF Support

The eBPF program already supported multiple cgroups via the `traced_cgroups` hash map (max 64 entries). The `AddTracedCgroup()` method allows adding multiple cgroup IDs at runtime.

### Helper Utilities

**pkg/cgroup/multi_container.go:**
- `DiscoverPodContainers()` - Lists all container cgroups in the current pod
- `FindContainerByName()` - Finds a specific container by name/ID pattern

These utilities can be used to build more sophisticated container discovery logic.

## Usage Examples

### Example 1: Trace All Containers in a Pod

Use the init container to discover all cgroups:

```yaml
initContainers:
  - name: cgroup-finder
    command:
      - sh
      - -c
      - |
        POD_CGROUP=$(dirname $(cat /proc/self/cgroup | cut -d: -f3))
        cd "/sys/fs/cgroup$POD_CGROUP"
        CGROUPS=""
        for dir in */; do
          CGROUP_PATH="$POD_CGROUP/${dir%/}"
          CGROUPS="${CGROUPS:+$CGROUPS,}$CGROUP_PATH"
        done
        echo "$CGROUPS" > /snoop-data/cgroup-paths
```

Then use in snoop:
```yaml
args:
  - -cgroups=$(cat /data/cgroup-paths)
```

### Example 2: Trace Specific Containers Only

Use a shell wrapper in the snoop container to filter:

```yaml
containers:
  - name: snoop
    command:
      - sh
      - -c
      - |
        # Discover and filter cgroups
        SELF_CGROUP=$(cat /proc/self/cgroup | cut -d: -f3)
        POD_CGROUP=$(dirname "$SELF_CGROUP")
        
        CGROUPS=""
        cd "/sys/fs/cgroup$POD_CGROUP"
        for dir in */; do
          CGROUP_PATH="$POD_CGROUP/${dir%/}"
          FULL_PATH="/sys/fs/cgroup$CGROUP_PATH"
          
          # Skip snoop's own cgroup
          if [ "$FULL_PATH" != "/sys/fs/cgroup$SELF_CGROUP" ]; then
            CGROUPS="${CGROUPS:+$CGROUPS,}$FULL_PATH"
          fi
        done
        
        exec /usr/local/bin/snoop -cgroups="$CGROUPS" # ... other args
```

### Example 3: Manually Specify Containers

If you know the container IDs, specify them directly:

```yaml
args:
  - -cgroups=/sys/fs/cgroup/kubepods/burstable/pod<uid>/cri-containerd-<id1>.scope,/sys/fs/cgroup/kubepods/burstable/pod<uid>/cri-containerd-<id2>.scope
```

## Complete Example

See `deploy/kubernetes/multi-container-example.yaml` for a complete working example with:
- Nginx web server (main app)
- Busybox log shipper (sidecar)
- Snoop sidecar (traces nginx and log-shipper, excludes itself)

## Testing

Run the tests:
```bash
go test ./pkg/config/
```

The test suite includes:
- Validation of single and multiple cgroup paths
- Backwards compatibility testing
- Parsing of comma-separated paths
- Migration from old to new field

## Backwards Compatibility

The implementation maintains full backwards compatibility:
- Old `-cgroup` flag still works
- Single path is automatically migrated to `CgroupPaths` slice
- Existing manifests and deployments continue to work without changes

## Documentation

Updated documentation:
- `deploy/kubernetes/README.md` - Added multi-container pod support section
- `deploy/kubernetes/multi-container-example.yaml` - Complete working example
- `plan.md` - Marked Milestone 4 as complete

## Future Enhancements

Potential improvements for future versions:
- Auto-discovery mode that automatically excludes snoop's own cgroup
- Container name-based filtering (via Kubernetes API or container runtime API)
- Dynamic cgroup discovery (watch for new containers starting)
- Pod annotation-based configuration (e.g., `snoop.io/trace-containers: "nginx,sidecar"`)
