#!/bin/bash
# Teardown script for KinD testing environment
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-snoop-test}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "================================================"
echo "Tearing down KinD test environment"
echo "================================================"
echo ""

# Check if cluster exists
if ! kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
    echo "Cluster $CLUSTER_NAME does not exist. Nothing to do."
    exit 0
fi

# Delete cluster
echo "Deleting KinD cluster: $CLUSTER_NAME"
kind delete cluster --name "$CLUSTER_NAME"

# Clean up temp files
if [ -f "$SCRIPT_DIR/.image-tag" ]; then
    rm "$SCRIPT_DIR/.image-tag"
fi

echo ""
echo "Teardown complete!"
