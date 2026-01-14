# Resource Limits and Recommendations

This document provides guidance on configuring resource limits for the snoop sidecar in production environments.

## Overview

Snoop is designed to be lightweight and have minimal impact on application performance. However, proper resource limits ensure predictable behavior and protect against resource exhaustion under unusual conditions.

## Memory Usage

### Components

Snoop's memory usage consists of several components:

1. **eBPF Maps (Kernel Memory)**
   - Ring buffer: 256 KB (fixed)
   - Per-CPU heap: 1 entry × ~300 bytes × CPU count (negligible)
   - Traced cgroups map: 64 entries × ~16 bytes = ~1 KB
   - Dropped events counter: negligible
   - **Total kernel memory: ~300 KB** (plus per-CPU overhead)

2. **Userspace Deduplication Cache**
   - Each file path entry: ~256 bytes (path string + map/list overhead)
   - Default (unbounded): memory grows with unique files seen
   - With `max-unique-files=N`: capped at approximately `N × 256 bytes`
   
   Examples:
   - 10,000 files: ~2.5 MB
   - 100,000 files: ~25 MB
   - 1,000,000 files: ~250 MB

3. **Go Runtime Overhead**
   - Base runtime: ~5-10 MB
   - Goroutines: minimal (4-5 goroutines)
   - HTTP server (metrics/health): ~1-2 MB

4. **Report Buffer**
   - JSON serialization buffer: proportional to unique files
   - Temporary, released after each report write
   - Peak usage: ~2× the deduplication cache size during report generation

### Recommendations

#### Conservative (Most Applications)
For applications with typical file access patterns (thousands of unique files):

```yaml
resources:
  requests:
    memory: 32Mi
  limits:
    memory: 128Mi
```

Configuration:
```bash
-max-unique-files=50000  # Cap at 50K unique files (~12 MB)
```

#### Moderate (Large Applications)
For applications that access many files (e.g., monorepos, data processing):

```yaml
resources:
  requests:
    memory: 64Mi
  limits:
    memory: 256Mi
```

Configuration:
```bash
-max-unique-files=200000  # Cap at 200K unique files (~50 MB)
```

#### Unbounded (Long-Running Observability)
For long-term observation where you want complete data:

```yaml
resources:
  requests:
    memory: 128Mi
  limits:
    memory: 512Mi
```

Configuration:
```bash
-max-unique-files=0  # Unbounded, monitor snoop_unique_files metric
```

**Important**: When using unbounded mode, actively monitor the `snoop_unique_files` metric to detect unexpected growth.

## CPU Usage

### Characteristics

Snoop's CPU usage is primarily driven by:

1. **Event Processing**: Proportional to syscall frequency
   - Each event: path normalization, deduplication lookup, metric updates
   - Typical overhead: <50 µs per event
   
2. **Report Writing**: Periodic JSON serialization
   - Frequency: configurable via `-interval` (default: 30s)
   - Duration: 1-10ms for typical workloads
   
3. **eBPF Overhead**: Kernel-side filtering and event emission
   - Per-syscall overhead: <1 µs
   - Negligible for most workloads

### Expected CPU Usage

| Workload | File Accesses/sec | Expected CPU | Notes |
|----------|-------------------|--------------|-------|
| Idle | <10 | <0.1% | Background syscalls only |
| Light | 10-100 | 0.1-0.5% | Typical web services |
| Moderate | 100-1000 | 0.5-2% | Active applications |
| Heavy | 1000-10000 | 2-5% | High I/O workloads |
| Extreme | >10000 | 5-10% | May hit ring buffer limits |

### Recommendations

#### Conservative (Most Applications)
```yaml
resources:
  requests:
    cpu: 10m      # 10 millicores (1% of 1 CPU)
  limits:
    cpu: 100m     # 100 millicores (10% of 1 CPU)
```

#### Moderate (High I/O Applications)
```yaml
resources:
  requests:
    cpu: 20m      # 20 millicores (2% of 1 CPU)
  limits:
    cpu: 200m     # 200 millicores (20% of 1 CPU)
```

#### High-Throughput (Data Processing)
```yaml
resources:
  requests:
    cpu: 50m      # 50 millicores (5% of 1 CPU)
  limits:
    cpu: 500m     # 500 millicores (50% of 1 CPU)
```

**Note**: CPU limits should be generous to avoid throttling during burst activity (e.g., application startup).

## Disk I/O

Snoop writes reports to disk periodically. The I/O pattern is:

- Frequency: Every `-interval` seconds (default: 30s)
- Write size: Proportional to unique files (typical: 10-500 KB per report)
- Method: Atomic write via temp file + rename
- Peak I/O: 2× the report size (temp file + rename)

### Recommendations

1. **Report Interval**: 
   - Default (30s) is appropriate for most workloads
   - Increase for large file counts to reduce I/O frequency:
     ```bash
     -interval=60s   # For 100K+ unique files
     -interval=120s  # For 500K+ unique files
     ```

2. **Volume Type**:
   - Any volume type is suitable (even NFS)
   - Atomic rename is used for crash safety
   - No special I/O performance requirements

3. **Volume Size**:
   - Minimum: 100 MB
   - Recommended: 1 GB (allows for log rotation, multiple reports)
   - Report size: typically 10-500 KB, max ~5 MB for 1M files

## Complete Examples

### Example 1: Small Web Service

Application characteristics:
- 2-3 containers per pod
- ~5,000 unique files accessed
- Light file I/O (<100 accesses/sec)

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: myapp
spec:
  containers:
  - name: app
    image: myapp:latest
    # ... app config ...
    
  - name: snoop
    image: snoop:latest
    args:
    - -cgroup=/sys/fs/cgroup/kubepods/pod$(POD_UID)/$(CONTAINER_ID)
    - -report=/data/snoop-report.json
    - -interval=30s
    - -max-unique-files=10000
    - -log-level=info
    securityContext:
      capabilities:
        add: [SYS_ADMIN, BPF, PERFMON]
      readOnlyRootFilesystem: true
    resources:
      requests:
        cpu: 10m
        memory: 32Mi
      limits:
        cpu: 100m
        memory: 128Mi
    volumeMounts:
    - name: snoop-data
      mountPath: /data
    - name: cgroup
      mountPath: /sys/fs/cgroup
      readOnly: true
      
  volumes:
  - name: snoop-data
    emptyDir:
      sizeLimit: 100Mi
  - name: cgroup
    hostPath:
      path: /sys/fs/cgroup
```

### Example 2: Data Processing Application

Application characteristics:
- Single container, processing large datasets
- ~100,000 unique files accessed
- Heavy file I/O (1,000+ accesses/sec)

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: data-processor
spec:
  containers:
  - name: app
    image: data-processor:latest
    # ... app config ...
    
  - name: snoop
    image: snoop:latest
    args:
    - -cgroup=/sys/fs/cgroup/kubepods/pod$(POD_UID)/$(CONTAINER_ID)
    - -report=/data/snoop-report.json
    - -interval=60s
    - -max-unique-files=200000
    - -log-level=info
    securityContext:
      capabilities:
        add: [SYS_ADMIN, BPF, PERFMON]
      readOnlyRootFilesystem: true
    resources:
      requests:
        cpu: 50m
        memory: 128Mi
      limits:
        cpu: 500m
        memory: 512Mi
    volumeMounts:
    - name: snoop-data
      mountPath: /data
    - name: cgroup
      mountPath: /sys/fs/cgroup
      readOnly: true
      
  volumes:
  - name: snoop-data
    emptyDir:
      sizeLimit: 500Mi
  - name: cgroup
    hostPath:
      path: /sys/fs/cgroup
```

### Example 3: Long-Running Observability

Application characteristics:
- Production service, running for weeks
- Unknown file access patterns (new deployment)
- Want complete observability without data loss

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: production-service
spec:
  containers:
  - name: app
    image: production-service:latest
    # ... app config ...
    
  - name: snoop
    image: snoop:latest
    args:
    - -cgroup=/sys/fs/cgroup/kubepods/pod$(POD_UID)/$(CONTAINER_ID)
    - -report=/data/snoop-report.json
    - -interval=60s
    - -max-unique-files=0  # Unbounded - monitor snoop_unique_files
    - -log-level=warn      # Reduce log noise in production
    - -metrics-addr=:9090
    securityContext:
      capabilities:
        add: [SYS_ADMIN, BPF, PERFMON]
      readOnlyRootFilesystem: true
    resources:
      requests:
        cpu: 20m
        memory: 128Mi
      limits:
        cpu: 200m
        memory: 1Gi  # Generous limit for unbounded mode
    volumeMounts:
    - name: snoop-data
      mountPath: /data
    - name: cgroup
      mountPath: /sys/fs/cgroup
      readOnly: true
    ports:
    - name: metrics
      containerPort: 9090
      
  volumes:
  - name: snoop-data
    persistentVolumeClaim:
      claimName: snoop-data-pvc  # Persistent storage for long-term observation
  - name: cgroup
    hostPath:
      path: /sys/fs/cgroup
```

## Monitoring and Alerts

Use Prometheus metrics to monitor snoop resource usage:

### Memory Monitoring

```promql
# Current unique files being tracked
snoop_unique_files

# Memory estimate (bytes): unique_files × 256
snoop_unique_files * 256

# Alert when approaching memory limits
snoop_unique_files * 256 > 100000000  # 100 MB
```

### CPU Monitoring

```promql
# Event processing rate
rate(snoop_events_received_total[5m])

# Events dropped (ring buffer overflow)
rate(snoop_events_dropped_total[5m])

# Cache evictions (memory pressure)
rate(snoop_events_evicted_total[5m])
```

### Recommended Alerts

```yaml
# Alert when ring buffer is dropping events
- alert: SnoopRingBufferOverflow
  expr: rate(snoop_events_dropped_total[5m]) > 10
  for: 5m
  annotations:
    summary: "Snoop is dropping events due to ring buffer overflow"
    description: "Consider increasing CPU limits or reducing file access rate"

# Alert when cache eviction is occurring
- alert: SnoopCacheEviction
  expr: rate(snoop_events_evicted_total[5m]) > 0
  for: 5m
  annotations:
    summary: "Snoop is evicting cached file paths"
    description: "Consider increasing max-unique-files or memory limits"

# Alert when memory usage is high
- alert: SnoopHighMemoryUsage
  expr: snoop_unique_files > 300000
  for: 10m
  annotations:
    summary: "Snoop is tracking a large number of unique files"
    description: "Consider setting max-unique-files limit or investigating unusual file access patterns"
```

## Tuning Guidelines

### Reducing Memory Usage

If memory usage is higher than expected:

1. **Set a limit**: Use `-max-unique-files` to cap memory growth
   ```bash
   -max-unique-files=50000  # Limit to ~12 MB
   ```

2. **Increase exclusions**: Filter out unnecessary paths
   ```bash
   -exclude=/proc/,/sys/,/dev/,/tmp/  # Add /tmp/ if temp files aren't relevant
   ```

3. **Monitor evictions**: Check if LRU evictions are affecting data completeness
   ```promql
   rate(snoop_events_evicted_total[1h])
   ```

### Reducing CPU Usage

If CPU usage is higher than expected:

1. **Increase report interval**: Reduce JSON serialization frequency
   ```bash
   -interval=60s  # or 120s
   ```

2. **Check event rate**: Verify application file access patterns
   ```promql
   rate(snoop_events_received_total[5m])
   ```

3. **Verify filtering**: Ensure cgroup filtering is working correctly
   ```bash
   # Check that only target container events are being processed
   # Log level debug will show which cgroup IDs are traced
   -log-level=debug
   ```

### Handling Ring Buffer Overflow

If `snoop_events_dropped_total` is increasing:

1. **This is expected under extreme load** (>10,000 file accesses/sec)
2. **Options**:
   - Increase CPU limits to process events faster
   - Accept data loss during burst periods (best-effort design)
   - For critical observability, consider increasing ring buffer size (requires recompilation)

## Performance Impact on Application

Snoop's design minimizes impact on the traced application:

- **Syscall overhead**: <1 µs per syscall (eBPF filtering is extremely fast)
- **No application changes**: Zero code changes required
- **Kernel-side filtering**: Only relevant cgroups emit events
- **Ring buffer**: Asynchronous event delivery, no blocking

Expected application performance impact: **<0.1%** for typical workloads.

### Verification

To verify snoop is not impacting your application:

1. **Before/after benchmarks**: Run application benchmarks with and without snoop
2. **Monitor application metrics**: Watch application-specific performance metrics
3. **Syscall latency**: Use `perf` to measure syscall latency changes

```bash
# Measure syscall latency without snoop
sudo perf stat -e 'syscalls:sys_enter_openat' -p $(pidof myapp)

# Compare with snoop enabled
```

## Troubleshooting

### High Memory Usage (Unbounded Mode)

**Symptom**: Memory grows continuously beyond expected levels

**Diagnosis**:
```promql
snoop_unique_files  # Check current file count
```

**Solutions**:
1. Verify application isn't accessing an unusual number of files
2. Check for symlink loops or recursive directory traversal
3. Add exclusions for problematic paths
4. Set `-max-unique-files` limit

### OOMKilled

**Symptom**: Snoop container is killed by OOM

**Diagnosis**: Check `kubectl describe pod` or container logs

**Solutions**:
1. Set or increase `-max-unique-files`
2. Increase memory limits
3. Reduce `-interval` so reports are written more frequently (releases memory during GC)
4. Add more path exclusions

### High CPU Usage

**Symptom**: Snoop using more CPU than expected

**Diagnosis**:
```promql
rate(snoop_events_received_total[5m])  # Check event rate
```

**Solutions**:
1. Verify event rate is reasonable for application
2. Check if filtering is correct (only target cgroup should be traced)
3. Increase CPU limits if processing legitimate high event rate
4. Reduce log level from `debug` to `info` or `warn`

### Events Dropped

**Symptom**: `snoop_events_dropped_total` is increasing

**Diagnosis**:
```promql
rate(snoop_events_dropped_total[5m])  # Check drop rate
```

**Solutions**:
1. This is **expected** under extreme load (>10K events/sec)
2. Increase CPU limits to process events faster
3. Accept data loss (snoop is designed as best-effort)
4. For critical observability, consider tuning ring buffer size (code change required)

## Summary

**Default Configuration** (suitable for 80% of workloads):
```yaml
resources:
  requests:
    cpu: 10m
    memory: 32Mi
  limits:
    cpu: 100m
    memory: 128Mi

args:
- -max-unique-files=50000
- -interval=30s
- -log-level=info
```

**Key Principles**:
1. Start conservative, scale up based on metrics
2. Always monitor `snoop_unique_files` in unbounded mode
3. CPU limits should be generous to handle burst activity
4. Ring buffer drops are acceptable under extreme load
5. Snoop is designed for best-effort observability, not guaranteed delivery
