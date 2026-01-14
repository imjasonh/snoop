# Kubernetes Deployment

This directory contains Kubernetes manifests for deploying snoop as a sidecar container.

## Files

- `rbac.yaml` - RBAC resources (ServiceAccount, ClusterRole, ClusterRoleBinding)
- `deployment.yaml` - Example deployment with snoop sidecar and test application
- `example-app.yaml` - Example showing how to add snoop to an nginx deployment
- `multi-container-example.yaml` - Example showing snoop in a multi-container pod (tracing specific containers)

## Prerequisites

- Kubernetes cluster with:
  - Linux kernel 5.4+ with eBPF support
  - BTF (BPF Type Format) enabled
  - cgroup v2 (most modern clusters)
  - containerd or CRI-O container runtime
- `kubectl` configured to access your cluster
- Node access to `/sys/fs/cgroup` and `/sys/kernel/debug`

## Quick Start

Deploy the example application with snoop sidecar:

```bash
# Apply RBAC resources
kubectl apply -f rbac.yaml

# Deploy the example
kubectl apply -f deployment.yaml

# Check the deployment
kubectl -n snoop-system get pods
kubectl -n snoop-system logs -f deployment/snoop-example -c snoop

# View the report (once the pod is running)
kubectl -n snoop-system exec -it deployment/snoop-example -c app -- cat /data/snoop-report.json

# Check metrics
kubectl -n snoop-system port-forward deployment/snoop-example 9090:9090
# Then open http://localhost:9090/metrics in your browser

# Clean up
kubectl delete -f deployment.yaml
kubectl delete -f rbac.yaml
```

## Adding Snoop to Your Application

To add snoop to an existing deployment, you need to:

### 1. Add the sidecar container

Add the snoop container to your pod spec:

```yaml
containers:
  - name: snoop
    image: ghcr.io/imjasonh/snoop:latest
    securityContext:
      privileged: false
      capabilities:
        add:
          - SYS_ADMIN
          - BPF
          - PERFMON
      readOnlyRootFilesystem: true
    env:
      - name: POD_NAME
        valueFrom:
          fieldRef:
            fieldPath: metadata.name
    command:
      - /usr/local/bin/snoop
    args:
      - -cgroup=/sys/fs/cgroup$(cat /data/cgroup-path)
      - -report=/data/snoop-report.json
      - -interval=30s
      - -exclude=/proc/,/sys/,/dev/
      - -metrics-addr=:9090
      - -log-level=info
      - -max-unique-files=100000
      - -container-id=$(POD_NAME)
    volumeMounts:
      - name: snoop-data
        mountPath: /data
      - name: cgroup
        mountPath: /sys/fs/cgroup
        readOnly: true
      - name: debugfs
        mountPath: /sys/kernel/debug
        readOnly: true
    ports:
      - name: metrics
        containerPort: 9090
    resources:
      requests:
        cpu: 50m
        memory: 64Mi
      limits:
        cpu: 200m
        memory: 128Mi
    livenessProbe:
      httpGet:
        path: /healthz
        port: 9090
      initialDelaySeconds: 10
      periodSeconds: 30
    readinessProbe:
      httpGet:
        path: /healthz
        port: 9090
      initialDelaySeconds: 5
      periodSeconds: 10
```

### 2. Add required volumes

```yaml
volumes:
  - name: snoop-data
    emptyDir: {}
  - name: cgroup
    hostPath:
      path: /sys/fs/cgroup
      type: Directory
  - name: debugfs
    hostPath:
      path: /sys/kernel/debug
      type: Directory
```

### 3. Add init container for cgroup discovery

```yaml
initContainers:
  - name: cgroup-finder
    image: busybox:latest
    command:
      - sh
      - -c
      - |
        if [ -f /proc/self/cgroup ]; then
          CGROUP_PATH=$(cat /proc/self/cgroup | cut -d: -f3)
          echo "Found cgroup path: $CGROUP_PATH"
          echo "$CGROUP_PATH" > /snoop-data/cgroup-path
        else
          echo "Could not determine cgroup path"
          exit 1
        fi
    volumeMounts:
      - name: snoop-data
        mountPath: /snoop-data
```

### 4. Add Prometheus annotations (optional)

```yaml
metadata:
  annotations:
    prometheus.io/scrape: "true"
    prometheus.io/port: "9090"
    prometheus.io/path: "/metrics"
```

See `example-app.yaml` for a complete example with nginx.

## Configuration

The snoop sidecar accepts the following command-line arguments:

| Argument | Default | Description |
|----------|---------|-------------|
| `-cgroup` | (required*) | Single cgroup path to trace |
| `-cgroups` | (required*) | Comma-separated list of cgroup paths (for multi-container pods) |
| `-report` | `/data/snoop-report.json` | Path to write JSON reports |
| `-interval` | `30s` | Interval between report writes |
| `-exclude` | `/proc/,/sys/,/dev/` | Comma-separated path prefixes to exclude |
| `-metrics-addr` | `:9090` | Address for metrics/health endpoint |
| `-log-level` | `info` | Log level (debug, info, warn, error) |
| `-max-unique-files` | `0` | Max unique files to track (0 = unbounded) |
| `-container-id` | (optional) | Container ID for report metadata |
| `-image` | (optional) | Image reference for report metadata |

*Either `-cgroup` or `-cgroups` must be specified.

## Multi-Container Pod Support

If your pod has multiple containers and you want to trace specific containers (not all), you can use the `-cgroups` flag with multiple paths:

### Method 1: Trace all containers in the pod

Modify the init container to discover all container cgroups:

```yaml
initContainers:
  - name: cgroup-finder
    image: busybox:latest
    command:
      - sh
      - -c
      - |
        # Get the pod cgroup (parent of our container)
        SELF_CGROUP=$(cat /proc/self/cgroup | cut -d: -f3)
        POD_CGROUP=$(dirname "$SELF_CGROUP")
        
        # List all container cgroups in the pod
        cd "/sys/fs/cgroup$POD_CGROUP"
        CGROUPS=""
        for dir in */; do
          if [ -d "$dir" ]; then
            CGROUP_PATH="$POD_CGROUP/${dir%/}"
            if [ -z "$CGROUPS" ]; then
              CGROUPS="$CGROUP_PATH"
            else
              CGROUPS="$CGROUPS,$CGROUP_PATH"
            fi
          fi
        done
        
        echo "$CGROUPS" > /snoop-data/cgroup-paths
        echo "Found cgroups: $CGROUPS"
    volumeMounts:
      - name: snoop-data
        mountPath: /snoop-data
      - name: cgroup
        mountPath: /sys/fs/cgroup
        readOnly: true
```

Then update the snoop args to use `-cgroups`:

```yaml
args:
  - -cgroups=$(cat /data/cgroup-paths)
  - -report=/data/snoop-report.json
  # ... other args
```

### Method 2: Trace specific containers by name pattern

If you know the container names or IDs, you can manually specify them:

```yaml
args:
  - -cgroups=/sys/fs/cgroup/kubepods/burstable/pod<uid>/<container1-id>,/sys/fs/cgroup/kubepods/burstable/pod<uid>/<container2-id>
  - -report=/data/snoop-report.json
  # ... other args
```

### Method 3: Exclude snoop's own container

To trace all containers except snoop itself:

```yaml
initContainers:
  - name: cgroup-finder
    image: busybox:latest
    command:
      - sh
      - -c
      - |
        # Get pod cgroup
        SELF_CGROUP=$(cat /proc/self/cgroup | cut -d: -f3)
        POD_CGROUP=$(dirname "$SELF_CGROUP")
        
        # Mark snoop's container ID to exclude it later
        # Snoop will be the last container started, we'll filter in snoop container
        echo "$POD_CGROUP" > /snoop-data/pod-cgroup
    volumeMounts:
      - name: snoop-data
        mountPath: /snoop-data
```

Then in the snoop container, use a wrapper script to discover and filter:

```yaml
containers:
  - name: snoop
    # ... other config
    command:
      - sh
      - -c
      - |
        # Discover all containers except self
        POD_CGROUP=$(cat /data/pod-cgroup)
        SELF_CGROUP=$(cat /proc/self/cgroup | cut -d: -f3)
        
        CGROUPS=""
        cd "/sys/fs/cgroup$POD_CGROUP"
        for dir in */; do
          CGROUP_PATH="$POD_CGROUP/${dir%/}"
          # Skip our own cgroup
          if [ "/sys/fs/cgroup$CGROUP_PATH" != "/sys/fs/cgroup$SELF_CGROUP" ]; then
            if [ -z "$CGROUPS" ]; then
              CGROUPS="/sys/fs/cgroup$CGROUP_PATH"
            else
              CGROUPS="$CGROUPS,/sys/fs/cgroup$CGROUP_PATH"
            fi
          fi
        done
        
        echo "Tracing cgroups: $CGROUPS"
        exec /usr/local/bin/snoop -cgroups="$CGROUPS" -report=/data/snoop-report.json # ... other args
```

**Note**: The third method (excluding snoop) is more complex but ensures snoop doesn't trace its own file access, which keeps reports cleaner.

## Security Considerations

The snoop sidecar requires elevated capabilities to load eBPF programs:

- `SYS_ADMIN` - Required for the `bpf()` syscall
- `BPF` - Explicit BPF capability (kernel 5.8+)
- `PERFMON` - For perf events (kernel 5.8+)

These capabilities are needed to observe file access, but snoop:

- Does NOT require `privileged: true`
- Uses `readOnlyRootFilesystem: true`
- Only reads from `/sys/fs/cgroup` and `/sys/kernel/debug`
- Writes reports to a dedicated volume
- Does not modify application behavior

## Troubleshooting

### Pod fails to start with "permission denied"

Check that your cluster allows the required security capabilities:

```bash
kubectl get psp  # For clusters using PodSecurityPolicy
kubectl describe psp <policy-name>
```

Or if using Pod Security Standards (Kubernetes 1.25+):

```bash
kubectl label namespace <namespace> pod-security.kubernetes.io/enforce=privileged
```

### Init container fails to find cgroup path

This usually means the pod is not using cgroup v2. Check your node:

```bash
kubectl debug node/<node-name> -it --image=alpine
mount | grep cgroup
```

You should see cgroup2 mounted at `/sys/fs/cgroup`.

### eBPF program fails to load

Check kernel version and BTF support:

```bash
kubectl debug node/<node-name> -it --image=alpine
uname -r  # Should be 5.4+
ls -la /sys/kernel/btf/vmlinux  # Should exist
```

### No events are being recorded

Check the snoop logs:

```bash
kubectl -n <namespace> logs -f <pod-name> -c snoop
```

Verify the cgroup path is correct:

```bash
kubectl -n <namespace> exec <pod-name> -c snoop -- cat /data/cgroup-path
```

### Metrics endpoint not accessible

Port-forward to the metrics port:

```bash
kubectl -n <namespace> port-forward <pod-name> 9090:9090
curl http://localhost:9090/metrics
curl http://localhost:9090/healthz
```

## Resource Usage

Typical resource usage for the snoop sidecar:

- **CPU**: 10-50m (idle), up to 200m under heavy load
- **Memory**: 32-64Mi baseline, grows with unique file count
  - ~100KB per 1000 unique files tracked
  - With `max-unique-files=100000`: ~74Mi maximum

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

For high-traffic applications, consider:

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 256Mi
```

## Monitoring

Snoop exposes Prometheus metrics on port 9090:

- `snoop_events_total` - Total events received by syscall type
- `snoop_events_processed_total` - Events that resulted in new files
- `snoop_events_duplicate_total` - Events for already-seen files
- `snoop_events_excluded_total` - Events filtered by exclusion rules
- `snoop_events_dropped_total` - Events dropped due to buffer overflow
- `snoop_events_evicted_total` - Files evicted from deduplication cache
- `snoop_unique_files` - Current count of unique files tracked
- `snoop_report_writes_total` - Number of successful report writes
- `snoop_report_write_errors_total` - Number of failed report writes

Health check endpoint:

- `GET /healthz` - Returns 200 OK if snoop is healthy

## Retrieving Reports

There are several ways to retrieve the generated reports:

### 1. Exec into the pod

```bash
kubectl exec <pod-name> -c app -- cat /data/snoop-report.json
```

### 2. Copy from the pod

```bash
kubectl cp <pod-name>:/data/snoop-report.json ./snoop-report.json -c app
```

### 3. Use a sidecar container to push reports

Add another sidecar that periodically uploads the report to an S3 bucket or API endpoint.

### 4. Mount a persistent volume

Replace the `emptyDir` with a `PersistentVolumeClaim` to retain reports across pod restarts.

## Next Steps

- Configure Prometheus to scrape the metrics endpoint
- Set up alerting for dropped events or high memory usage
- Aggregate reports from multiple pods for analysis
- Use the reports to identify unused files and slim your container images

For more information, see the main [project documentation](../../README.md).
