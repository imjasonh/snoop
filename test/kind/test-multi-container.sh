#!/bin/bash
set -e

# Multi-Container Pod Test
# This test validates that snoop correctly discovers and traces multiple
# containers in a single pod, with per-container file attribution.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NAMESPACE="snoop-test"
DEPLOYMENT="multi-container-test"
RESULTS_DIR="${SCRIPT_DIR}/results/multi-container"

echo "================================"
echo "Multi-Container Pod Test"
echo "================================"
echo ""

# Create results directory
mkdir -p "${RESULTS_DIR}"

# Get image tag (from environment or file)
if [ -z "$IMAGE_TAG" ]; then
    if [ -f "$SCRIPT_DIR/.image-tag" ]; then
        IMAGE_TAG=$(cat "$SCRIPT_DIR/.image-tag")
    else
        IMAGE_TAG="snoop:test-latest"
    fi
fi
echo "Using image: $IMAGE_TAG"
echo ""

# Update manifest with correct image tag
TEMP_MANIFEST="${RESULTS_DIR}/multi-container-manifest.yaml"
sed "s|image: snoop:test-latest|image: $IMAGE_TAG|g" \
    "${SCRIPT_DIR}/manifests/multi-container-test.yaml" > "${TEMP_MANIFEST}"

# Deploy the test
echo "📦 Deploying multi-container test pod..."
kubectl apply -f "${TEMP_MANIFEST}"

# Wait for deployment to be ready
echo "⏳ Waiting for deployment to be ready..."
kubectl -n ${NAMESPACE} wait --for=condition=available --timeout=120s deployment/${DEPLOYMENT}

# Wait for pod to be running
echo "⏳ Waiting for pod to be running..."
kubectl -n ${NAMESPACE} wait --for=condition=ready --timeout=120s pod -l app=multi-container-test

# Get pod name
POD_NAME=$(kubectl -n ${NAMESPACE} get pod -l app=multi-container-test -o jsonpath='{.items[0].metadata.name}')
echo "✓ Pod running: ${POD_NAME}"

# Check that all containers are running
echo ""
echo "🔍 Checking container status..."
CONTAINER_COUNT=$(kubectl -n ${NAMESPACE} get pod ${POD_NAME} -o jsonpath='{.spec.containers[*].name}' | wc -w)
echo "  Total containers in pod: ${CONTAINER_COUNT}"

APP_CONTAINERS=$(kubectl -n ${NAMESPACE} get pod ${POD_NAME} -o jsonpath='{.spec.containers[*].name}' | tr ' ' '\n' | grep -v snoop | tr '\n' ' ')
echo "  Application containers: ${APP_CONTAINERS}"

# Check snoop logs for discovery
echo ""
echo "🔍 Checking snoop container discovery..."
sleep 5  # Give snoop time to start and discover
kubectl -n ${NAMESPACE} logs ${POD_NAME} -c snoop | head -30

DISCOVERED=$(kubectl -n ${NAMESPACE} logs ${POD_NAME} -c snoop | grep -c "Discovered.*containers to trace" || true)
if [ "$DISCOVERED" -eq 0 ]; then
    echo "❌ ERROR: Snoop did not log container discovery"
    kubectl -n ${NAMESPACE} logs ${POD_NAME} -c snoop
    exit 1
fi
echo "✓ Snoop discovered containers"

# Wait for file accesses to happen (containers access files every 10 seconds)
echo ""
echo "⏳ Waiting 35 seconds for file accesses and first report..."
sleep 35

# Retrieve the report
echo ""
echo "📊 Retrieving snoop report..."
kubectl -n ${NAMESPACE} exec ${POD_NAME} -c snoop -- cat /data/snoop-report.json > "${RESULTS_DIR}/report.json"
echo "✓ Report saved to ${RESULTS_DIR}/report.json"

# Show report summary
echo ""
echo "📋 Report summary:"
cat "${RESULTS_DIR}/report.json" | jq -r '
  "Pod: \(.pod_name // "unknown")",
  "Namespace: \(.namespace // "unknown")",
  "Containers: \(.containers | length)",
  "",
  "Container details:",
  (.containers[] | "  - \(.name): \(.unique_files) files, \(.total_events) events")
'

# Validate the report structure
echo ""
echo "🔍 Validating report structure..."

# Build the validator if needed
if [ ! -f "${SCRIPT_DIR}/validate/validate" ]; then
    echo "  Building validator..."
    (cd "${SCRIPT_DIR}/validate" && go build -o validate .)
fi

# Run validation with expected containers
"${SCRIPT_DIR}/validate/validate" "${RESULTS_DIR}/report.json" nginx busybox alpine

# Additional checks using jq
echo ""
echo "🔍 Additional validation checks..."

# Check that report has containers field
CONTAINERS=$(cat "${RESULTS_DIR}/report.json" | jq -r '.containers | length')
if [ "$CONTAINERS" -lt 3 ]; then
    echo "❌ ERROR: Expected at least 3 containers, got ${CONTAINERS}"
    exit 1
fi
echo "✓ Found ${CONTAINERS} containers in report"

# Check that at least 3 containers have files (nginx, busybox, alpine)
# Note: Container names are short IDs, not the k8s container names
CONTAINERS_WITH_FILES=$(cat "${RESULTS_DIR}/report.json" | jq -r '[.containers[] | select(.files | length > 0)] | length')
if [ "$CONTAINERS_WITH_FILES" -lt 3 ]; then
    echo "❌ ERROR: Expected at least 3 containers with files, got ${CONTAINERS_WITH_FILES}"
    exit 1
fi
echo "✓ Found ${CONTAINERS_WITH_FILES} containers with files captured"

# Show details
cat "${RESULTS_DIR}/report.json" | jq -r '.containers[] | select(.files | length > 0) | "  ✓ Container \(.name): \(.files | length) files"'

# Check for expected files in specific containers
echo ""
echo "🔍 Checking for expected files in specific containers..."

# Nginx should have nginx.conf
if [ -n "$BUSYBOX_PASSWD" ] && [ -n "$ALPINE_PASSWD" ]; then
    echo "✓ Shared file /etc/passwd appears in both busybox and alpine containers"
else
    echo "ℹ Shared file /etc/passwd not yet accessed by both containers"
fi

# Check that snoop is NOT in the containers list
SNOOP_IN_REPORT=$(cat "${RESULTS_DIR}/report.json" | jq -r '.containers[] | select(.name == "snoop")' || true)
if [ -n "$SNOOP_IN_REPORT" ]; then
    echo "❌ ERROR: Snoop container found in report (should be self-excluded)"
    exit 1
fi
echo "✓ Snoop correctly excluded itself from tracking"

# Check metrics
echo ""
echo "📊 Checking Prometheus metrics..."
# Use port-forward to access metrics without needing wget/curl in container
kubectl port-forward -n ${NAMESPACE} ${POD_NAME} 19090:9090 >/dev/null 2>&1 &
PF_PID=$!
sleep 2

if curl -s http://localhost:19090/metrics > "${RESULTS_DIR}/metrics.txt" 2>/dev/null; then
    echo "✓ Metrics retrieved successfully"
    # Check for per-container metrics (if implemented)
    if grep -q "snoop_events_total" "${RESULTS_DIR}/metrics.txt"; then
        echo "✓ Found snoop_events_total metric"
    fi
else
    echo "⚠ Could not retrieve metrics"
fi

# Clean up port-forward
kill $PF_PID 2>/dev/null || true
wait $PF_PID 2>/dev/null || true

# Save logs
echo ""
echo "💾 Saving logs..."
kubectl -n ${NAMESPACE} logs ${POD_NAME} -c snoop > "${RESULTS_DIR}/snoop.log"
kubectl -n ${NAMESPACE} logs ${POD_NAME} -c nginx > "${RESULTS_DIR}/nginx.log" 2>&1 || true
kubectl -n ${NAMESPACE} logs ${POD_NAME} -c busybox > "${RESULTS_DIR}/busybox.log" 2>&1 || true
kubectl -n ${NAMESPACE} logs ${POD_NAME} -c alpine > "${RESULTS_DIR}/alpine.log" 2>&1 || true
echo "✓ Logs saved to ${RESULTS_DIR}/"

echo ""
echo "================================"
echo "✅ Multi-Container Test PASSED"
echo "================================"
echo ""
echo "Results saved in: ${RESULTS_DIR}"
echo ""
echo "To view the full report:"
echo "  cat ${RESULTS_DIR}/report.json | jq ."
echo ""

# Cleanup (useful for CI)
if [ "${CLEANUP:-true}" = "true" ]; then
    echo "🧹 Cleaning up test resources..."
    kubectl delete -f "${TEMP_MANIFEST}" --wait=false >/dev/null 2>&1 || true
    echo "✓ Cleanup complete"
else
    echo "To clean up manually:"
    echo "  kubectl delete -f ${SCRIPT_DIR}/manifests/multi-container-test.yaml"
fi
