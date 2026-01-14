# KinD Testing Plan for Snoop

**Status**: Pre-Milestone 4 Completion  
**Priority**: URGENT  
**Created**: 2026-01-14

## Overview

This document articulates the comprehensive testing plan for validating snoop's Kubernetes integration using KinD (Kubernetes in Docker). This testing pre-empts Helm deployment and other Milestone 4 deliverables, ensuring the core sidecar functionality works correctly in a real Kubernetes environment.

## Goals

1. **Validate Core Functionality**: Verify snoop correctly traces file access in a Kubernetes pod
2. **Test Metadata Enrichment**: Confirm pod/namespace/image metadata appears in reports
3. **Verify Sidecar Pattern**: Ensure snoop and application containers coexist properly
4. **Test Multiple Workload Types**: Validate with different application patterns (nginx, alpine, Python, etc.)
5. **Check Production Readiness**: Validate health checks, metrics, graceful shutdown
6. **Identify Gaps**: Find any issues in current manifests or documentation

## Pre-requisites

### Local Environment

- **macOS**: Development machine (cannot build eBPF, but can orchestrate tests)
- **Docker Desktop**: For running KinD cluster
- **KinD**: v0.20.0+ installed (`go install sigs.k8s.io/kind@latest`)
- **kubectl**: v1.28+ configured
- **ko**: For building container images (`go install github.com/google/ko@latest`)

### Required Tools

```bash
# Install KinD
go install sigs.k8s.io/kind@latest

# Install kubectl (if not already installed)
# On macOS:
brew install kubectl

# Install ko
go install github.com/google/ko@latest

# Install jq for JSON processing
brew install jq

# Optional: k9s for cluster monitoring
brew install k9s
```

### Build Environment

Since eBPF code generation requires Linux, we need:

1. **Option A**: Use the existing Dockerfile multi-stage build (handles eBPF generation)
2. **Option B**: Pre-built images from CI (if available)
3. **Option C**: Build in a Linux container/VM

We'll use **Option A** (Dockerfile) initially since it's already set up.

## Test Infrastructure

### 1. KinD Cluster Configuration

Create a KinD cluster with specific settings for eBPF:

**File**: `test/kind/cluster-config.yaml`

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: snoop-test

# Use a recent node image with eBPF support
nodes:
  - role: control-plane
    image: kindest/node:v1.29.0
    extraMounts:
      # Mount kernel debug filesystem (required for eBPF)
      - hostPath: /sys/kernel/debug
        containerPath: /sys/kernel/debug
        readOnly: false
      # Mount cgroup filesystem
      - hostPath: /sys/fs/cgroup
        containerPath: /sys/fs/cgroup
        readOnly: false

# Enable feature gates if needed
featureGates:
  # None required for basic eBPF
```

### 2. Test Applications

Create multiple test workloads with different file access patterns:

#### Test App 1: Simple Alpine Loop
- **Purpose**: Basic file access validation
- **File Access Pattern**: Read `/etc/passwd`, `/etc/hosts`, list `/usr/bin`
- **Expected Files**: ~10-20 files

#### Test App 2: Nginx Web Server
- **Purpose**: Real-world application
- **File Access Pattern**: Config files, logs, static content
- **Expected Files**: ~50-100 files

#### Test App 3: Python Flask App
- **Purpose**: Interpreted language with many imports
- **File Access Pattern**: Python stdlib, site-packages, application code
- **Expected Files**: ~200-500 files

#### Test App 4: Busybox Scripted
- **Purpose**: Controlled, predictable file access
- **File Access Pattern**: Explicitly access specific files
- **Expected Files**: Exactly known list

### 3. Validation Scripts

Create Go-based validation tools (prefer Go over bash):

**File**: `test/kind/validate/main.go`

```go
package main

// Validates:
// 1. Report JSON structure is correct
// 2. Expected files are present
// 3. Excluded files are absent
// 4. Metadata fields are populated
// 5. Metrics endpoint is accessible
// 6. Health check endpoint works
```

### 4. Test Orchestration

**File**: `test/kind/run-tests.sh`

```bash
#!/bin/bash
# Main test runner that:
# 1. Creates KinD cluster
# 2. Builds and loads snoop image
# 3. Deploys test applications
# 4. Waits for reports
# 5. Validates results
# 6. Cleans up
```

## Test Scenarios

### Scenario 1: Basic Sidecar Deployment

**Objective**: Verify snoop can run alongside a simple application.

**Steps**:
1. Create KinD cluster with config
2. Build snoop image: `ko build --local ./cmd/snoop`
3. Load image into KinD: `kind load docker-image <image>`
4. Apply RBAC: `kubectl apply -f deploy/kubernetes/rbac.yaml`
5. Deploy test app (alpine loop): `kubectl apply -f test/kind/manifests/alpine-test.yaml`
6. Wait for pod ready: `kubectl wait --for=condition=Ready pod -l app=alpine-test`
7. Check snoop logs: `kubectl logs -l app=alpine-test -c snoop --tail=50`
8. Wait 35 seconds (for report interval)
9. Retrieve report: `kubectl cp <pod>:/data/snoop-report.json ./report-alpine.json`
10. Validate report structure and content

**Expected Results**:
- Pod reaches Ready state
- Snoop logs show "eBPF probes attached"
- Report JSON is valid
- Report contains `/etc/passwd`, `/etc/hosts`, `/usr/bin/*`
- Report excludes `/proc/`, `/sys/`, `/dev/`
- Metadata fields populated: `pod_name`, `namespace`

**Success Criteria**:
- ✅ Pod healthy
- ✅ Report generated within 35 seconds
- ✅ At least 10 files captured
- ✅ No excluded files present
- ✅ Metadata correct

### Scenario 2: Nginx Application

**Objective**: Test with a real-world web server.

**Steps**:
1. Deploy nginx with snoop: `kubectl apply -f deploy/kubernetes/example-app.yaml`
2. Wait for pod ready
3. Generate traffic: `kubectl exec -it <pod> -c nginx -- wget -O /dev/null http://localhost/`
4. Wait for report
5. Retrieve and validate report

**Expected Results**:
- nginx starts successfully
- Snoop captures nginx config files: `/etc/nginx/nginx.conf`
- Snoop captures nginx binary: `/usr/sbin/nginx`
- Snoop captures shared libraries: `/lib/*.so*`
- Cache directory `/var/cache/nginx` excluded (per exclusion config)

**Success Criteria**:
- ✅ nginx responds to HTTP requests
- ✅ Report contains nginx-specific files
- ✅ Cache files excluded
- ✅ Metrics endpoint accessible: `snoop_events_total` > 0

### Scenario 3: Python Flask Application

**Objective**: Test with interpreted language and many imports.

**Steps**:
1. Deploy Flask app with snoop: `kubectl apply -f test/kind/manifests/flask-test.yaml`
2. Wait for pod ready
3. Make HTTP request to trigger imports
4. Wait for report
5. Validate large file list

**Expected Results**:
- Flask app starts and serves requests
- Report contains Python interpreter: `/usr/bin/python3`
- Report contains stdlib modules: `/usr/lib/python3.X/...`
- Report contains Flask package files
- Potentially 200-500 files captured

**Success Criteria**:
- ✅ Flask app responds correctly
- ✅ Report contains Python-related files
- ✅ File count > 100
- ✅ Memory usage < 128Mi (check with `kubectl top pod`)

### Scenario 4: Multi-Container Pod

**Objective**: Verify snoop traces only target container, not sidecar container.

**Steps**:
1. Deploy pod with: app container + snoop sidecar + logging sidecar
2. Verify snoop doesn't trace its own file access
3. Verify snoop doesn't trace logging sidecar
4. Verify snoop only traces app container

**Expected Results**:
- Report contains files from app container only
- Snoop binary `/usr/local/bin/snoop` NOT in report
- Logging sidecar files NOT in report

**Success Criteria**:
- ✅ Only app container files present
- ✅ Snoop's own files excluded
- ✅ Other sidecar files excluded

### Scenario 5: Health and Metrics

**Objective**: Validate production readiness endpoints.

**Steps**:
1. Deploy any test app with snoop
2. Port-forward metrics: `kubectl port-forward <pod> 9090:9090`
3. Check health: `curl http://localhost:9090/healthz`
4. Check metrics: `curl http://localhost:9090/metrics`
5. Validate Prometheus format

**Expected Results**:
- Health endpoint returns `200 OK`
- Metrics endpoint returns Prometheus format
- Key metrics present:
  - `snoop_events_total`
  - `snoop_unique_files`
  - `snoop_report_writes_total`
- Process metrics present:
  - `process_cpu_seconds_total`
  - `process_resident_memory_bytes`

**Success Criteria**:
- ✅ `/healthz` returns 200
- ✅ `/metrics` returns valid Prometheus output
- ✅ All expected metrics present with values > 0

### Scenario 6: Graceful Shutdown

**Objective**: Verify final report written on pod termination.

**Steps**:
1. Deploy test app with snoop
2. Wait for first report
3. Trigger pod deletion: `kubectl delete pod <pod>`
4. Capture logs during shutdown
5. Retrieve final report before pod terminates

**Expected Results**:
- Snoop logs show "Received signal: terminated"
- Snoop logs show "Writing final report"
- Final report exists and is valid
- No data loss

**Success Criteria**:
- ✅ Graceful shutdown logged
- ✅ Final report written
- ✅ Exit code 0

### Scenario 7: Cgroup Discovery

**Objective**: Validate cgroup path discovery mechanism.

**Steps**:
1. Check init container logs for cgroup path
2. Verify cgroup path is correct format
3. Compare with actual pod cgroup path on node
4. Ensure snoop uses correct path

**Expected Results**:
- Init container finds cgroup path
- Path format: `/kubepods/.../<pod-uid>/...`
- Snoop successfully attaches to correct cgroup

**Success Criteria**:
- ✅ Init container succeeds
- ✅ Cgroup path file created
- ✅ Snoop uses correct path
- ✅ Events captured

### Scenario 8: Resource Limits

**Objective**: Verify snoop operates within resource constraints.

**Steps**:
1. Deploy with strict resource limits (50m CPU, 64Mi memory)
2. Generate high file access load
3. Monitor resource usage: `kubectl top pod`
4. Check for OOM kills or throttling

**Expected Results**:
- CPU usage stays below 100m under normal load
- Memory usage stays below 64Mi with bounded deduplication
- No OOM kills
- Metrics show if events dropped

**Success Criteria**:
- ✅ No OOM kills
- ✅ CPU < 100m average
- ✅ Memory < 80Mi
- ✅ `snoop_events_dropped_total` = 0 (or documented)

### Scenario 9: Report Format Validation

**Objective**: Validate report JSON structure matches specification.

**Steps**:
1. Retrieve report from any test
2. Parse JSON with validation tool
3. Check required fields present
4. Check data types correct
5. Check timestamps valid

**Expected Report Structure**:
```json
{
  "container_id": "alpine-test-xxxxx",
  "image_ref": "alpine:latest",
  "image_digest": "",
  "pod_name": "alpine-test-xxxxx",
  "namespace": "default",
  "labels": {},
  "started_at": "2026-01-14T12:00:00Z",
  "last_updated_at": "2026-01-14T12:00:35Z",
  "files": [
    "/etc/passwd",
    "/etc/hosts",
    "/usr/bin/ls"
  ],
  "total_events": 150,
  "dropped_events": 0
}
```

**Success Criteria**:
- ✅ Valid JSON
- ✅ All required fields present
- ✅ Timestamps in RFC3339 format
- ✅ Files array non-empty
- ✅ No duplicate files in array

### Scenario 10: Path Normalization

**Objective**: Verify path normalization works correctly.

**Steps**:
1. Deploy busybox container with script that accesses:
   - Relative paths: `./file.txt`
   - Parent directories: `../etc/passwd`
   - Current directory: `.`
   - Redundant slashes: `/usr//bin/ls`
2. Check report paths are normalized

**Expected Results**:
- All paths are absolute
- No `.` or `..` components
- No redundant slashes
- Symlinks NOT resolved (record what app asked for)

**Success Criteria**:
- ✅ All paths start with `/`
- ✅ No relative path components
- ✅ Paths are clean and canonical

## Test Environment Setup

### Directory Structure

```
test/
└── kind/
    ├── cluster-config.yaml          # KinD cluster configuration
    ├── run-tests.sh                 # Main test orchestration script
    ├── setup.sh                     # Cluster setup
    ├── teardown.sh                  # Cleanup
    ├── manifests/
    │   ├── alpine-test.yaml         # Simple alpine test
    │   ├── busybox-script.yaml      # Controlled file access
    │   ├── flask-test.yaml          # Python Flask app
    │   └── multi-container.yaml     # Multi-container pod test
    ├── validate/
    │   ├── main.go                  # Report validation tool
    │   ├── report.go                # Report structure definition
    │   └── checks.go                # Validation checks
    └── results/
        └── .gitkeep                 # Directory for test results
```

### Cluster Setup Script

**File**: `test/kind/setup.sh`

```bash
#!/bin/bash
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-snoop-test}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Creating KinD cluster: $CLUSTER_NAME"
kind create cluster --config "$SCRIPT_DIR/cluster-config.yaml" --name "$CLUSTER_NAME"

echo "Building snoop image with ko..."
# ko build handles the multi-stage Docker build internally
export KO_DOCKER_REPO=kind.local
IMAGE=$(ko build ./cmd/snoop)

echo "Loading image into KinD cluster..."
kind load docker-image "$IMAGE" --name "$CLUSTER_NAME"

echo "Applying RBAC..."
kubectl apply -f "$SCRIPT_DIR/../../deploy/kubernetes/rbac.yaml"

echo "Cluster ready for testing!"
echo "Image: $IMAGE"
```

### Test Runner Script

**File**: `test/kind/run-tests.sh`

```bash
#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RESULTS_DIR="$SCRIPT_DIR/results"
mkdir -p "$RESULTS_DIR"

# Track results
PASSED=0
FAILED=0

run_test() {
    local test_name="$1"
    local manifest="$2"
    local validator="$3"
    
    echo ""
    echo "========================================="
    echo "Running: $test_name"
    echo "========================================="
    
    # Deploy
    kubectl apply -f "$manifest"
    
    # Wait for ready
    POD_LABEL=$(grep -A1 "matchLabels:" "$manifest" | tail -1 | awk '{print $2}')
    kubectl wait --for=condition=Ready pod -l "app=$POD_LABEL" --timeout=60s
    
    # Wait for report interval + buffer
    echo "Waiting 40 seconds for report generation..."
    sleep 40
    
    # Get pod name
    POD_NAME=$(kubectl get pod -l "app=$POD_LABEL" -o jsonpath='{.items[0].metadata.name}')
    
    # Retrieve report
    REPORT_FILE="$RESULTS_DIR/${test_name}-report.json"
    kubectl cp "$POD_NAME:/data/snoop-report.json" "$REPORT_FILE" -c app || {
        echo "❌ FAILED: Could not retrieve report"
        kubectl logs "$POD_NAME" -c snoop --tail=50
        ((FAILED++))
        return 1
    }
    
    # Validate
    echo "Validating report..."
    if "$validator" "$REPORT_FILE"; then
        echo "✅ PASSED: $test_name"
        ((PASSED++))
    else
        echo "❌ FAILED: $test_name"
        ((FAILED++))
    fi
    
    # Cleanup
    kubectl delete -f "$manifest" --wait=false
}

# Build validator tool
echo "Building validation tool..."
(cd "$SCRIPT_DIR/validate" && go build -o "$RESULTS_DIR/validate" .)

# Run test scenarios
run_test "alpine-basic" "$SCRIPT_DIR/manifests/alpine-test.yaml" "$RESULTS_DIR/validate"
run_test "nginx-real-world" "$SCRIPT_DIR/../../deploy/kubernetes/example-app.yaml" "$RESULTS_DIR/validate"
run_test "busybox-controlled" "$SCRIPT_DIR/manifests/busybox-script.yaml" "$RESULTS_DIR/validate"

# Summary
echo ""
echo "========================================="
echo "Test Summary"
echo "========================================="
echo "Passed: $PASSED"
echo "Failed: $FAILED"
echo ""

if [ "$FAILED" -eq 0 ]; then
    echo "✅ All tests passed!"
    exit 0
else
    echo "❌ Some tests failed"
    exit 1
fi
```

### Validation Tool

**File**: `test/kind/validate/main.go`

```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Report struct {
	ContainerID   string            `json:"container_id"`
	ImageRef      string            `json:"image_ref"`
	PodName       string            `json:"pod_name"`
	Namespace     string            `json:"namespace"`
	Labels        map[string]string `json:"labels"`
	StartedAt     time.Time         `json:"started_at"`
	LastUpdatedAt time.Time         `json:"last_updated_at"`
	Files         []string          `json:"files"`
	TotalEvents   uint64            `json:"total_events"`
	DroppedEvents uint64            `json:"dropped_events"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <report.json>\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}

	reportPath := os.Args[1]
	if err := validateReport(reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "Validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Report validation passed")
}

func validateReport(path string) error {
	// Read and parse JSON
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading report: %w", err)
	}

	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	// Validate required fields
	if report.PodName == "" {
		return fmt.Errorf("pod_name is empty")
	}
	if report.Namespace == "" {
		return fmt.Errorf("namespace is empty")
	}
	if report.StartedAt.IsZero() {
		return fmt.Errorf("started_at is zero")
	}
	if report.LastUpdatedAt.IsZero() {
		return fmt.Errorf("last_updated_at is zero")
	}

	// Validate files array
	if len(report.Files) == 0 {
		return fmt.Errorf("files array is empty")
	}

	// Check for excluded paths
	excludedPrefixes := []string{"/proc/", "/sys/", "/dev/"}
	for _, file := range report.Files {
		for _, prefix := range excludedPrefixes {
			if strings.HasPrefix(file, prefix) {
				return fmt.Errorf("excluded file found: %s", file)
			}
		}
		
		// Check paths are absolute
		if !strings.HasPrefix(file, "/") {
			return fmt.Errorf("relative path found: %s", file)
		}
		
		// Check for path components that should be normalized
		if strings.Contains(file, "/./") || strings.Contains(file, "/../") {
			return fmt.Errorf("non-normalized path: %s", file)
		}
	}

	// Check for duplicates
	seen := make(map[string]bool)
	for _, file := range report.Files {
		if seen[file] {
			return fmt.Errorf("duplicate file: %s", file)
		}
		seen[file] = true
	}

	// Validate timestamps
	if report.LastUpdatedAt.Before(report.StartedAt) {
		return fmt.Errorf("last_updated_at before started_at")
	}

	// Success
	fmt.Printf("  - Files captured: %d\n", len(report.Files))
	fmt.Printf("  - Total events: %d\n", report.TotalEvents)
	fmt.Printf("  - Dropped events: %d\n", report.DroppedEvents)
	fmt.Printf("  - Pod: %s/%s\n", report.Namespace, report.PodName)

	return nil
}
```

## Test Manifests

### Alpine Test

**File**: `test/kind/manifests/alpine-test.yaml`

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: snoop-test
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: alpine-test
  namespace: snoop-test
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: alpine-test
  namespace: snoop-test
spec:
  replicas: 1
  selector:
    matchLabels:
      app: alpine-test
  template:
    metadata:
      labels:
        app: alpine-test
    spec:
      serviceAccountName: alpine-test
      volumes:
        - name: snoop-data
          emptyDir: {}
        - name: cgroup
          hostPath:
            path: /sys/fs/cgroup
        - name: debugfs
          hostPath:
            path: /sys/kernel/debug
      initContainers:
        - name: cgroup-finder
          image: busybox:latest
          command:
            - sh
            - -c
            - |
              CGROUP_PATH=$(cat /proc/self/cgroup | cut -d: -f3)
              echo "$CGROUP_PATH" > /snoop-data/cgroup-path
              echo "Cgroup path: $CGROUP_PATH"
          volumeMounts:
            - name: snoop-data
              mountPath: /snoop-data
      containers:
        - name: app
          image: alpine:latest
          command:
            - sh
            - -c
            - |
              echo "Starting file access test..."
              while true; do
                cat /etc/passwd > /dev/null
                cat /etc/hosts > /dev/null
                ls /usr/bin > /dev/null
                ls /lib > /dev/null
                sleep 5
              done
          volumeMounts:
            - name: snoop-data
              mountPath: /data
              readOnly: true
        - name: snoop
          image: kind.local/snoop:latest
          imagePullPolicy: Never
          securityContext:
            capabilities:
              add: [SYS_ADMIN, BPF, PERFMON]
            readOnlyRootFilesystem: true
          env:
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          command: ["/usr/local/bin/snoop"]
          args:
            - -cgroup=/sys/fs/cgroup$(cat /data/cgroup-path)
            - -report=/data/snoop-report.json
            - -interval=30s
            - -exclude=/proc/,/sys/,/dev/
            - -metrics-addr=:9090
            - -log-level=info
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

### Busybox Controlled Test

**File**: `test/kind/manifests/busybox-script.yaml`

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: snoop-test
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: busybox-test
  namespace: snoop-test
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: busybox-script
  namespace: snoop-test
data:
  test.sh: |
    #!/bin/sh
    echo "Starting controlled file access test..."
    
    # Create test file
    echo "test content" > /tmp/test.txt
    
    # Test various access patterns
    while true; do
      # Absolute paths
      cat /etc/passwd > /dev/null
      cat /etc/hosts > /dev/null
      
      # Relative paths (should be normalized)
      cd /etc
      cat ./passwd > /dev/null
      cat ../etc/hosts > /dev/null
      
      # Executable access
      /bin/ls /usr > /dev/null
      
      # Temp file
      cat /tmp/test.txt > /dev/null
      
      echo "Cycle complete"
      sleep 5
    done
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: busybox-test
  namespace: snoop-test
spec:
  replicas: 1
  selector:
    matchLabels:
      app: busybox-test
  template:
    metadata:
      labels:
        app: busybox-test
    spec:
      serviceAccountName: busybox-test
      volumes:
        - name: snoop-data
          emptyDir: {}
        - name: cgroup
          hostPath:
            path: /sys/fs/cgroup
        - name: debugfs
          hostPath:
            path: /sys/kernel/debug
        - name: script
          configMap:
            name: busybox-script
            defaultMode: 0755
      initContainers:
        - name: cgroup-finder
          image: busybox:latest
          command:
            - sh
            - -c
            - |
              CGROUP_PATH=$(cat /proc/self/cgroup | cut -d: -f3)
              echo "$CGROUP_PATH" > /snoop-data/cgroup-path
          volumeMounts:
            - name: snoop-data
              mountPath: /snoop-data
      containers:
        - name: app
          image: busybox:latest
          command: ["/scripts/test.sh"]
          volumeMounts:
            - name: snoop-data
              mountPath: /data
              readOnly: true
            - name: script
              mountPath: /scripts
        - name: snoop
          image: kind.local/snoop:latest
          imagePullPolicy: Never
          securityContext:
            capabilities:
              add: [SYS_ADMIN, BPF, PERFMON]
            readOnlyRootFilesystem: true
          env:
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
          command: ["/usr/local/bin/snoop"]
          args:
            - -cgroup=/sys/fs/cgroup$(cat /data/cgroup-path)
            - -report=/data/snoop-report.json
            - -interval=30s
            - -exclude=/proc/,/sys/,/dev/
            - -metrics-addr=:9090
            - -log-level=debug
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
```

## Execution Plan

### Phase 1: Infrastructure Setup (Day 1)

1. **Create test directory structure**
   - `test/kind/` with subdirectories
   - Commit structure to git

2. **Write KinD cluster configuration**
   - `cluster-config.yaml` with eBPF mounts
   - Test cluster creation manually

3. **Create setup/teardown scripts**
   - `setup.sh` for cluster + image build
   - `teardown.sh` for cleanup
   - Test scripts manually

4. **Build validation tool**
   - Write `validate/main.go`
   - Test with sample report JSON
   - Build and verify works

### Phase 2: Basic Tests (Day 1-2)

5. **Create alpine test manifest**
   - Simple loop accessing known files
   - Deploy manually and verify

6. **Create busybox test manifest**
   - Controlled file access script
   - Test path normalization

7. **Test with existing example-app.yaml**
   - Nginx deployment
   - Verify real-world scenario

8. **Write test runner script**
   - Orchestrate test execution
   - Run all three tests
   - Collect results

### Phase 3: Advanced Tests (Day 2)

9. **Create multi-container test**
   - Pod with app + snoop + logging
   - Verify isolation

10. **Test health/metrics endpoints**
    - Port-forward and curl
    - Validate Prometheus format

11. **Test graceful shutdown**
    - Delete pod during operation
    - Capture final report

12. **Test resource limits**
    - Deploy with strict limits
    - Monitor with `kubectl top`

### Phase 4: Documentation and Gaps (Day 2-3)

13. **Document findings**
    - Issues discovered
    - Gaps in manifests
    - Areas needing improvement

14. **Update manifests based on findings**
    - Fix any bugs found
    - Improve configurations

15. **Create comprehensive test report**
    - What works
    - What doesn't
    - Next steps

## Expected Issues to Investigate

Based on the manifests review, potential issues to validate:

### 1. Cgroup Discovery Mechanism

**Current Approach**: Init container writes cgroup path to file, snoop reads it via `$(cat /data/cgroup-path)`

**Potential Issues**:
- Shell expansion `$(cat ...)` in args might not work in all scenarios
- Race condition if snoop starts before file written
- Need to verify this works in KinD

**Test**: Check init container logs, verify file created, verify snoop reads correctly

### 2. Image Reference

**Current Approach**: Specified as arg `-image=nginx:1.25-alpine`

**Potential Issues**:
- Manual specification error-prone
- Doesn't capture actual image digest
- Need automated way to get this

**Test**: Verify image ref in report matches deployed image

### 3. Host Path Mounts

**Current Approach**: Direct hostPath mounts for `/sys/fs/cgroup` and `/sys/kernel/debug`

**Potential Issues**:
- KinD node paths might differ from real nodes
- Permissions issues possible
- Security implications

**Test**: Verify mounts work in KinD, check permissions

### 4. Target Container Identification

**Current Approach**: Manual cgroup path construction

**Potential Issues**:
- Fragile path construction
- Doesn't handle multi-container pods well
- Need better discovery mechanism

**Test**: Multi-container pod scenario, verify correct targeting

### 5. RBAC Permissions

**Current Approach**: ClusterRole with pod/node read access

**Potential Issues**:
- Might need more permissions for pod metadata
- ClusterRole might be too broad for some deployments

**Test**: Verify RBAC works, check if all permissions needed

## Success Metrics

This testing phase is successful if:

1. **✅ All 10 scenarios pass** with validation
2. **✅ Reports contain expected files** for each test case
3. **✅ Metadata is correctly populated** (pod name, namespace)
4. **✅ Health/metrics endpoints work** as documented
5. **✅ No security violations** or permission errors
6. **✅ Documentation gaps identified** and listed
7. **✅ Performance within targets** (<100m CPU, <128Mi memory)
8. **✅ Graceful shutdown works** correctly
9. **✅ Path normalization verified** working
10. **✅ Exclusions working** as configured

## Deliverables

After completing this testing plan:

1. **Test Infrastructure** (`test/kind/` directory)
   - Cluster configuration
   - Test manifests
   - Validation tools
   - Test runner scripts

2. **Test Results Document** (`KIND_TEST_RESULTS.md`)
   - Results for each scenario
   - Issues found
   - Performance metrics
   - Screenshots/logs

3. **Issue List** (GitHub issues or TODO list)
   - Bugs to fix
   - Improvements needed
   - Documentation gaps

4. **Updated Manifests** (if issues found)
   - Fixed deployment.yaml
   - Fixed example-app.yaml
   - Improved documentation

5. **Recommendations Document**
   - What works well
   - What needs improvement before Helm chart
   - Prerequisites for next milestone

## Timeline

- **Day 1 (4-6 hours)**: Infrastructure setup, basic tests
- **Day 2 (4-6 hours)**: Advanced tests, issue investigation
- **Day 3 (2-4 hours)**: Documentation, fixes, recommendations

**Total Effort**: ~12-16 hours

**Target Completion**: Before proceeding with Helm chart development

## Next Steps After Testing

Once this testing plan is complete and issues addressed:

1. ✅ Mark current Milestone 4 tasks as complete (if tests pass)
2. 🔄 Fix any issues found during testing
3. 📝 Update documentation based on findings
4. ➡️ Proceed with Helm chart development
5. ➡️ Test Helm deployment in KinD
6. ➡️ Test on real cluster (GKE/EKS)
7. ➡️ Move to Milestone 5 (Multi-Deployment Aggregation)

## References

- [KinD Quick Start](https://kind.sigs.k8s.io/docs/user/quick-start/)
- [KinD Configuration](https://kind.sigs.k8s.io/docs/user/configuration/)
- [ko Documentation](https://ko.build/)
- [eBPF on Kubernetes](https://cilium.io/blog/2020/11/10/ebpf-future-of-networking/)
- [Kubernetes Downward API](https://kubernetes.io/docs/tasks/inject-data-application/downward-api-volume-expose-pod-information/)
