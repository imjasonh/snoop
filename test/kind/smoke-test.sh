#!/bin/bash
# Quick smoke test - just verify one test works
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Running quick smoke test..."
echo ""

# Check cluster exists
if ! kind get clusters 2>/dev/null | grep -q "^snoop-test$"; then
    echo "Error: Run ./setup.sh first"
    exit 1
fi

# Build validator
echo "Building validator..."
(cd "$SCRIPT_DIR/validate" && go build -o "$SCRIPT_DIR/results/validate" .)

# Get image tag
IMAGE_TAG=$(cat "$SCRIPT_DIR/.image-tag")
echo "Using image: $IMAGE_TAG"
echo ""

# Update manifest with image
TEMP_MANIFEST="$SCRIPT_DIR/results/smoke-test-manifest.yaml"
sed "s|image: snoop:test-latest|image: $IMAGE_TAG|g" \
    "$SCRIPT_DIR/manifests/alpine-test.yaml" > "$TEMP_MANIFEST"

echo "Deploying alpine test..."
kubectl apply -f "$TEMP_MANIFEST"

echo "Waiting for pod to be ready..."
if ! kubectl wait --for=condition=Ready pod -l app=alpine-test -n snoop-test --timeout=90s; then
    echo "ERROR: Pod not ready"
    kubectl get pods -n snoop-test
    kubectl describe pod -l app=alpine-test -n snoop-test
    exit 1
fi

POD_NAME=$(kubectl get pod -l app=alpine-test -n snoop-test -o jsonpath='{.items[0].metadata.name}')
echo "Pod ready: $POD_NAME"
echo ""

echo "Checking snoop logs..."
kubectl logs -n snoop-test "$POD_NAME" -c snoop --tail=20
echo ""

echo "Waiting 35 seconds for report..."
sleep 35

echo "Retrieving report..."
if ! kubectl cp "snoop-test/$POD_NAME:/data/snoop-report.json" "$SCRIPT_DIR/results/smoke-report.json" -c app; then
    echo "ERROR: Could not retrieve report"
    echo ""
    echo "Snoop logs:"
    kubectl logs -n snoop-test "$POD_NAME" -c snoop
    echo ""
    echo "Checking /data directory:"
    kubectl exec -n snoop-test "$POD_NAME" -c app -- ls -la /data/
    exit 1
fi

echo "Report retrieved!"
echo ""

echo "Validating report..."
if ! "$SCRIPT_DIR/results/validate" "$SCRIPT_DIR/results/smoke-report.json"; then
    echo "ERROR: Validation failed"
    exit 1
fi

echo ""
echo "Cleaning up..."
kubectl delete -f "$TEMP_MANIFEST" --wait=false

echo ""
echo "✅ Smoke test passed!"
