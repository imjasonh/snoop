#!/bin/bash
# Helper script to find the cgroup path for a Docker container

if [ -z "$1" ]; then
    echo "Usage: $0 <container-name-or-id>"
    echo ""
    echo "Example: $0 snoop-app-1"
    echo ""
    echo "Available containers:"
    docker ps --format "table {{.Names}}\t{{.ID}}"
    exit 1
fi

CONTAINER=$1

# Get the container ID (works with both name and ID)
CONTAINER_ID=$(docker inspect --format='{{.Id}}' "$CONTAINER" 2>/dev/null)

if [ -z "$CONTAINER_ID" ]; then
    echo "Error: Container '$CONTAINER' not found"
    exit 1
fi

# Get the cgroup path
# For Docker with cgroup v2, it's typically in the systemd hierarchy
CGROUP_PATH=$(docker inspect --format='{{.HostConfig.CgroupParent}}' "$CONTAINER_ID" 2>/dev/null)

if [ -z "$CGROUP_PATH" ]; then
    # Try to find it from /proc
    PID=$(docker inspect --format='{{.State.Pid}}' "$CONTAINER_ID")
    if [ -n "$PID" ]; then
        CGROUP_PATH=$(cat /proc/$PID/cgroup | grep '^0::' | cut -d: -f3)
    fi
fi

if [ -z "$CGROUP_PATH" ]; then
    echo "Error: Could not determine cgroup path for container '$CONTAINER'"
    exit 1
fi

echo "Container: $CONTAINER"
echo "Container ID: $CONTAINER_ID"
echo "Cgroup Path: $CGROUP_PATH"
echo ""
echo "To trace this container, run snoop with:"
echo "  snoop -cgroup '$CGROUP_PATH'"
