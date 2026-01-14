#!/bin/bash
# Setup script for KinD testing environment
set -euo pipefail

# Accept optional image tag as first argument
PROVIDED_IMAGE_TAG="${1:-}"
CLUSTER_NAME="${CLUSTER_NAME:-snoop-test}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

echo "================================================"
echo "Setting up KinD test environment for snoop"
echo "================================================"
echo ""

# Check prerequisites
echo "Checking prerequisites..."
command -v kind >/dev/null 2>&1 || { echo "Error: kind not found. Install with: go install sigs.k8s.io/kind@latest"; exit 1; }
command -v kubectl >/dev/null 2>&1 || { echo "Error: kubectl not found"; exit 1; }
command -v docker >/dev/null 2>&1 || { echo "Error: docker not found"; exit 1; }

# Check if cluster already exists
if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    echo "Cluster $CLUSTER_NAME already exists. Deleting..."
    kind delete cluster --name "$CLUSTER_NAME"
fi

# Create cluster
echo ""
echo "Creating KinD cluster: $CLUSTER_NAME"
kind create cluster --config "$SCRIPT_DIR/cluster-config.yaml" --name "$CLUSTER_NAME"

echo ""
echo "Waiting for cluster to be ready..."
kubectl wait --for=condition=Ready nodes --all --timeout=120s

# Check node kernel version and eBPF support
echo ""
echo "Checking eBPF support..."
echo "Kernel version:"
kubectl debug node/snoop-test-control-plane -it --image=alpine -- uname -r 2>/dev/null || echo "  (debug check skipped)"

echo ""
echo "Checking for BTF support:"
kubectl debug node/snoop-test-control-plane -it --image=alpine -- ls -la /sys/kernel/btf/vmlinux 2>/dev/null || echo "  (debug check skipped)"

# Build or use provided snoop image
echo ""
if [ -n "$PROVIDED_IMAGE_TAG" ]; then
    echo "Using provided image: $PROVIDED_IMAGE_TAG"
    IMAGE_TAG="$PROVIDED_IMAGE_TAG"
    # Verify image exists
    if ! docker image inspect "$IMAGE_TAG" >/dev/null 2>&1; then
        echo "Error: Image $IMAGE_TAG does not exist. Please build it first."
        exit 1
    fi
else
    echo "Building snoop image..."
    echo "Note: Building on macOS, eBPF generation happens in Docker multi-stage build"

    cd "$PROJECT_ROOT"

    # Use Docker to build with the existing Dockerfile
    # This handles the eBPF code generation inside a Linux container
    IMAGE_TAG="snoop:test-$(date +%s)"
    echo "Building $IMAGE_TAG..."
    docker build -t "$IMAGE_TAG" -f Dockerfile .
fi

echo ""
echo "Loading image into KinD cluster..."
kind load docker-image "$IMAGE_TAG" --name "$CLUSTER_NAME"

# Apply RBAC
echo ""
echo "Applying RBAC resources..."
kubectl apply -f "$PROJECT_ROOT/deploy/kubernetes/rbac.yaml"

echo ""
echo "================================================"
echo "Setup complete!"
echo "================================================"
echo ""
echo "Cluster: $CLUSTER_NAME"
echo "Image: $IMAGE_TAG"
echo ""
echo "Next steps:"
echo "  1. Run tests: ./run-tests.sh"
echo "  2. Or deploy manually: kubectl apply -f manifests/alpine-test.yaml"
echo "  3. View logs: kubectl -n snoop-test logs -l app=alpine-test -c snoop"
echo ""
echo "To tear down: ./teardown.sh"
echo ""

# Save image tag for test runner
echo "$IMAGE_TAG" > "$SCRIPT_DIR/.image-tag"
