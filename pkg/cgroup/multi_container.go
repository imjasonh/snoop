package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DiscoverPodContainers finds all container cgroups within the current pod.
// This is useful for multi-container pods where you want to trace specific containers.
// Returns a map of container name (or ID suffix) to cgroup path.
func DiscoverPodContainers() (map[string]string, error) {
	// Read our own cgroup to determine the pod cgroup prefix
	selfCgroupData, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return nil, fmt.Errorf("reading /proc/self/cgroup: %w", err)
	}

	// Parse cgroup v2 format: 0::/path/to/cgroup
	var selfCgroupPath string
	for _, line := range strings.Split(string(selfCgroupData), "\n") {
		if strings.HasPrefix(line, "0::") {
			selfCgroupPath = strings.TrimPrefix(line, "0::")
			break
		}
	}

	if selfCgroupPath == "" {
		return nil, fmt.Errorf("cgroup v2 not found in /proc/self/cgroup")
	}

	// The pod cgroup is typically the parent directory
	// Path format: /kubepods/burstable/pod<uid>/<container-id>
	// We want to find all siblings (other containers in same pod)
	podCgroupPath := filepath.Dir(selfCgroupPath)

	// Read all subdirectories in the pod cgroup
	fullPodPath := filepath.Join("/sys/fs/cgroup", podCgroupPath)
	entries, err := os.ReadDir(fullPodPath)
	if err != nil {
		return nil, fmt.Errorf("reading pod cgroup directory %s: %w", fullPodPath, err)
	}

	containers := make(map[string]string)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip special cgroup control directories
		name := entry.Name()
		if strings.HasPrefix(name, "cgroup.") {
			continue
		}

		// Container directories are typically long hex IDs
		// Extract a meaningful name if possible, otherwise use short ID
		containerPath := filepath.Join(podCgroupPath, name)

		// Try to get a short identifier (last 12 chars of ID)
		shortID := name
		if len(name) > 12 {
			shortID = name[len(name)-12:]
		}

		containers[shortID] = containerPath
	}

	return containers, nil
}

// FindContainerByName finds a container cgroup path by matching a name pattern.
// The pattern can be a container ID prefix, full ID, or part of the container name.
// Returns the full cgroup path or an error if not found or multiple matches.
func FindContainerByName(pattern string) (string, error) {
	containers, err := DiscoverPodContainers()
	if err != nil {
		return "", err
	}

	var matches []string
	for id, path := range containers {
		if strings.Contains(id, pattern) || strings.Contains(path, pattern) {
			matches = append(matches, path)
		}
	}

	if len(matches) == 0 {
		return "", fmt.Errorf("no container found matching pattern %q", pattern)
	}

	if len(matches) > 1 {
		return "", fmt.Errorf("multiple containers found matching pattern %q: %v", pattern, matches)
	}

	return matches[0], nil
}
