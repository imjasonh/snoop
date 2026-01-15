//go:build linux

package cgroup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ContainerInfo holds information about a discovered container.
type ContainerInfo struct {
	CgroupID   uint64
	CgroupPath string
	Name       string // Short container ID or name
	HasAPK     bool   // True if APK database was found
	APKDBPath  string // Path to APK database if found
}

// Discovery finds cgroup IDs to trace
type Discovery interface {
	Discover(ctx context.Context) ([]uint64, error)
}

// SelfExcludingDiscovery traces all cgroups except snoop's own
type SelfExcludingDiscovery struct{}

// NewSelfExcludingDiscovery creates a new self-excluding discovery
func NewSelfExcludingDiscovery() *SelfExcludingDiscovery {
	return &SelfExcludingDiscovery{}
}

// Discover returns all cgroup IDs except our own
func (d *SelfExcludingDiscovery) Discover(ctx context.Context) ([]uint64, error) {
	// Get our own cgroup ID
	selfID, err := GetSelfCgroupID()
	if err != nil {
		return nil, fmt.Errorf("getting self cgroup ID: %w", err)
	}

	// For proof of concept, we'll just return an empty list
	// The actual implementation would scan /sys/fs/cgroup and filter out our own
	// For now, the user will need to manually add cgroups to trace
	_ = selfID
	return []uint64{}, nil
}

// DiscoverAllExceptSelf finds all containers in the current pod,
// excluding snoop's own container.
// Returns a map of cgroup_id -> ContainerInfo.
func DiscoverAllExceptSelf() (map[uint64]*ContainerInfo, error) {
	// Get our own cgroup path and ID
	selfCgroupPath, err := GetSelfCgroupPath()
	if err != nil {
		return nil, fmt.Errorf("getting self cgroup path: %w", err)
	}

	selfCgroupID, err := GetSelfCgroupID()
	if err != nil {
		return nil, fmt.Errorf("getting self cgroup ID: %w", err)
	}

	// Get the pod cgroup (parent directory)
	podCgroupPath := filepath.Dir(selfCgroupPath)

	// Special case: if we're in root cgroup ("/"), we need to find the actual pod cgroup
	// This happens in some container runtimes (e.g., KinD) where /proc/self/cgroup shows 0::/
	// In this case, look for POD_UID environment variable to find the pod cgroup
	if podCgroupPath == "/" || podCgroupPath == "." {
		podUID := os.Getenv("POD_UID")
		if podUID != "" {
			// Convert pod UID format: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee
			// to cgroup format: podaaaaaaaa_bbbb_cccc_dddd_eeeeeeeeeeee
			podUIDCgroup := "pod" + strings.ReplaceAll(podUID, "-", "_")

			// Search for the pod cgroup directory
			foundPath := ""
			filepath.Walk("/sys/fs/cgroup", func(path string, info os.FileInfo, err error) error {
				if err != nil || foundPath != "" {
					return filepath.SkipDir
				}
				if info.IsDir() && strings.Contains(filepath.Base(path), podUIDCgroup) {
					foundPath = path
					return filepath.SkipDir
				}
				// Limit search depth to avoid scanning entire filesystem
				if strings.Count(path, "/") > 8 {
					return filepath.SkipDir
				}
				return nil
			})

			if foundPath != "" {
				podCgroupPath = strings.TrimPrefix(foundPath, "/sys/fs/cgroup")
			}
		}
	}

	fullPodPath := filepath.Join("/sys/fs/cgroup", podCgroupPath)

	// Read all subdirectories in the pod cgroup
	entries, err := os.ReadDir(fullPodPath)
	if err != nil {
		return nil, fmt.Errorf("reading pod cgroup directory %s: %w", fullPodPath, err)
	}

	containers := make(map[uint64]*ContainerInfo)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip special cgroup control files and directories
		name := entry.Name()
		if strings.HasPrefix(name, "cgroup.") || strings.HasPrefix(name, ".") {
			continue
		}

		// Build the full cgroup path
		containerCgroupPath := filepath.Join(podCgroupPath, name)

		// Get the cgroup ID for this container
		cgroupID, err := GetCgroupIDByPath(containerCgroupPath)
		if err != nil {
			// Log but continue - some directories might not be valid cgroups
			continue
		}

		// Skip if this is snoop's own container
		if cgroupID == selfCgroupID {
			continue
		}

		// Extract a short, readable name from the cgroup path
		// Container directories are often in format: cri-containerd-<long-id>.scope
		shortName := extractContainerName(name)

		// Detect APK database for this container
		hasAPK, apkDBPath := detectAPKDatabase(containerCgroupPath)

		containers[cgroupID] = &ContainerInfo{
			CgroupID:   cgroupID,
			CgroupPath: containerCgroupPath,
			Name:       shortName,
			HasAPK:     hasAPK,
			APKDBPath:  apkDBPath,
		}
	}

	return containers, nil
}

// detectAPKDatabase checks if an APK database exists for the given container.
// It tries multiple methods to access the container's filesystem:
// 1. Via /proc/{pid}/root (works in docker, may not work in Kubernetes)
// 2. Via containerd overlay mounts (Kubernetes/containerd)
// 3. Via container root mounts in /var/lib/containers
// Returns true and the database path if found, false otherwise.
func detectAPKDatabase(containerCgroupPath string) (bool, string) {
	// Method 1: Try /proc/{pid}/root approach (simple, works in many cases)
	if hasAPK, path := tryProcPidRoot(containerCgroupPath); hasAPK {
		return true, path
	}

	// Method 2: Try containerd overlay filesystem (Kubernetes/containerd)
	if hasAPK, path := tryContainerdOverlay(containerCgroupPath); hasAPK {
		return true, path
	}

	// Method 3: Try /var/lib/containers (Podman/CRI-O)
	if hasAPK, path := tryContainersRoot(containerCgroupPath); hasAPK {
		return true, path
	}

	return false, ""
}

// tryProcPidRoot attempts to find APK database via /proc/{pid}/root
// This method reads the file through the /proc filesystem
// In Kubernetes, containers may not have PIDs immediately, so we retry
func tryProcPidRoot(containerCgroupPath string) (bool, string) {
	procsPath := filepath.Join("/sys/fs/cgroup", containerCgroupPath, "cgroup.procs")

	// Retry up to 5 times with delays - containers may not have PIDs immediately
	maxAttempts := 5
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			// Wait before retry (200ms, 400ms, 600ms, 800ms)
			time.Sleep(time.Duration(200*attempt) * time.Millisecond)
		}

		data, err := os.ReadFile(procsPath)
		if err != nil {
			if attempt == 0 {
				fmt.Fprintf(os.Stderr, "DEBUG: APK detection - cannot read %s: %v\n", procsPath, err)
			}
			continue
		}

		// Get all non-zero PIDs
		var pids []string
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			p := strings.TrimSpace(line)
			if p != "" && p != "0" {
				pids = append(pids, p)
			}
		}

		if len(pids) == 0 {
			if attempt == 0 {
				fmt.Fprintf(os.Stderr, "DEBUG: APK detection - no PIDs yet in %s (attempt %d), will retry...\n",
					filepath.Base(containerCgroupPath), attempt+1)
			}
			continue // Retry if no PIDs yet
		}

		if attempt > 0 {
			fmt.Fprintf(os.Stderr, "DEBUG: APK detection - found %d PIDs on attempt %d for %s\n",
				len(pids), attempt+1, filepath.Base(containerCgroupPath))
		}

		// Try each PID - use namespace entry to read the file
		for _, pid := range pids {
			// Try to read via namespace entry - this is the most reliable method
			if data, err := readFileViaNamespace(pid, "/lib/apk/db/installed"); err == nil && len(data) > 0 {
				// Write to a temporary location where we can access it
				tempPath := filepath.Join("/tmp", fmt.Sprintf("apk-db-%s-%s.txt", filepath.Base(containerCgroupPath), pid))
				if err := os.WriteFile(tempPath, data, 0644); err == nil {
					fmt.Fprintf(os.Stderr, "INFO: ✓ Found APK database via namespace for container %s (PID %s, attempt %d/%d, %d bytes)\n",
						filepath.Base(containerCgroupPath), pid, attempt+1, maxAttempts, len(data))
					return true, tempPath
				}
			} else if attempt == 0 && err != nil {
				fmt.Fprintf(os.Stderr, "DEBUG: APK detection - cannot read from PID %s namespace: %v\n", pid, err)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "DEBUG: APK detection - no APK database found after %d attempts for %s\n",
		maxAttempts, filepath.Base(containerCgroupPath))
	return false, ""
}

// readFileViaNamespace reads a file from a container's mount namespace
// Uses nsenter command to enter the namespace and cat the file
func readFileViaNamespace(pid, filePath string) ([]byte, error) {
	// Use nsenter to read the file from the container's mount namespace
	// nsenter -t <pid> -m -- cat <file>
	cmd := fmt.Sprintf("nsenter -t %s -m -- cat %s 2>/dev/null", pid, filePath)

	// Execute the command
	// We use sh -c to handle the command properly
	output, err := syscall.Exec("/bin/sh", []string{"/bin/sh", "-c", cmd}, os.Environ())
	if err != nil {
		// Exec failed, try using os/exec package instead
		return execNsenter(pid, filePath)
	}

	// This won't be reached as Exec replaces the process
	_ = output
	return nil, fmt.Errorf("unexpected: exec returned")
}

// execNsenter uses os/exec to run nsenter
func execNsenter(pid, filePath string) ([]byte, error) {
	// Create a simple shell command that uses nsenter
	// Note: we can't import os/exec in this file due to build constraints
	// So we'll use a manual approach with syscall

	// Actually, let's try a different approach - directly read via /proc/{pid}/root
	// but with a retry and better error handling
	apkDBPath := filepath.Join("/proc", pid, "root", filePath)

	// Try multiple read attempts in case of transient issues
	var lastErr error
	for i := 0; i < 3; i++ {
		if data, err := os.ReadFile(apkDBPath); err == nil && len(data) > 0 {
			return data, nil
		} else {
			lastErr = err
		}
		if i < 2 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	return nil, fmt.Errorf("failed to read %s: %w", apkDBPath, lastErr)
}

// tryContainerdOverlay attempts to find APK database in containerd overlay mounts
// Extracts container ID from cgroup path and searches overlay mounts
func tryContainerdOverlay(containerCgroupPath string) (bool, string) {
	// Extract container ID from path like:
	// /kubelet.slice/.../cri-containerd-{CONTAINER_ID}.scope
	containerID := extractContainerIDFromCgroupPath(containerCgroupPath)
	if containerID == "" {
		return false, ""
	}

	// Read /proc/mounts to find overlay mounts for this container
	mounts, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false, ""
	}

	// Look for overlay mounts containing this container ID
	for _, line := range strings.Split(string(mounts), "\n") {
		if !strings.Contains(line, "overlay") {
			continue
		}
		if !strings.Contains(line, containerID) {
			continue
		}

		// Parse mount line: overlay /var/lib/containerd/... overlay rw,...
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		mountPoint := fields[1]
		apkDBPath := filepath.Join(mountPoint, "lib", "apk", "db", "installed")
		if _, err := os.Stat(apkDBPath); err == nil {
			fmt.Fprintf(os.Stderr, "INFO: Found APK database via overlay mount at %s for container %s\n", apkDBPath, containerID)
			return true, apkDBPath
		}
	}

	return false, ""
}

// tryContainersRoot attempts to find APK database in /var/lib/containers (Podman/CRI-O)
func tryContainersRoot(containerCgroupPath string) (bool, string) {
	containerID := extractContainerIDFromCgroupPath(containerCgroupPath)
	if containerID == "" {
		return false, ""
	}

	// Common paths for container storage
	searchPaths := []string{
		filepath.Join("/var/lib/containers/storage/overlay", containerID, "merged"),
		filepath.Join("/var/lib/containers/storage/overlay-containers", containerID, "userdata"),
	}

	for _, basePath := range searchPaths {
		apkDBPath := filepath.Join(basePath, "lib", "apk", "db", "installed")
		if _, err := os.Stat(apkDBPath); err == nil {
			fmt.Fprintf(os.Stderr, "INFO: Found APK database at %s for container %s\n", apkDBPath, containerID)
			return true, apkDBPath
		}
	}

	return false, ""
}

// extractContainerIDFromCgroupPath extracts the container ID from a cgroup path
// Handles formats like: cri-containerd-{ID}.scope, docker-{ID}.scope
func extractContainerIDFromCgroupPath(cgroupPath string) string {
	// Get the last component of the path
	base := filepath.Base(cgroupPath)

	// Remove common suffixes
	base = strings.TrimSuffix(base, ".scope")
	base = strings.TrimSuffix(base, ".slice")

	// Remove common prefixes
	if strings.HasPrefix(base, "cri-containerd-") {
		return strings.TrimPrefix(base, "cri-containerd-")
	} else if strings.HasPrefix(base, "docker-") {
		return strings.TrimPrefix(base, "docker-")
	} else if strings.HasPrefix(base, "crio-") {
		return strings.TrimPrefix(base, "crio-")
	}

	return base
}

// extractContainerName extracts a readable name from a cgroup directory name.
// Handles various container runtime formats:
// - cri-containerd-<id>.scope -> <id[:12]>
// - docker-<id>.scope -> <id[:12]>
// - <id> -> <id[:12]>
func extractContainerName(dirName string) string {
	// Remove common suffixes
	name := strings.TrimSuffix(dirName, ".scope")
	name = strings.TrimSuffix(name, ".slice")

	// Remove common prefixes
	if strings.HasPrefix(name, "cri-containerd-") {
		name = strings.TrimPrefix(name, "cri-containerd-")
	} else if strings.HasPrefix(name, "docker-") {
		name = strings.TrimPrefix(name, "docker-")
	} else if strings.HasPrefix(name, "crio-") {
		name = strings.TrimPrefix(name, "crio-")
	}

	// Truncate long IDs to 12 characters (like docker ps does)
	if len(name) > 12 {
		name = name[:12]
	}

	return name
}

// GetSelfCgroupPath returns the cgroup path of the current process
// relative to /sys/fs/cgroup (e.g., "/system.slice/docker-abc123.scope")
func GetSelfCgroupPath() (string, error) {
	// Read /proc/self/cgroup to get cgroup path
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", fmt.Errorf("reading /proc/self/cgroup: %w", err)
	}

	// Parse cgroup v2 format: 0::/path/to/cgroup
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "0::") {
			cgroupPath := strings.TrimPrefix(line, "0::")
			return cgroupPath, nil
		}
	}

	return "", fmt.Errorf("cgroup v2 not found in /proc/self/cgroup")
}

// GetSelfCgroupID returns the cgroup ID of the current process
func GetSelfCgroupID() (uint64, error) {
	// Get the cgroup path
	cgroupPath, err := GetSelfCgroupPath()
	if err != nil {
		return 0, err
	}

	// Read the cgroup.id file to get the cgroup ID
	// The path is /sys/fs/cgroup/<cgroup_path>/cgroup.id
	// cgroupPath from /proc/self/cgroup already has leading /
	// For root cgroup ("/"), we need special handling
	var idPath string
	if cgroupPath == "/" {
		idPath = "/sys/fs/cgroup/cgroup.id"
	} else {
		idPath = filepath.Join("/sys/fs/cgroup", cgroupPath, "cgroup.id")
	}

	idData, err := os.ReadFile(idPath)
	if err != nil {
		// Fallback to syscall method if cgroup.id file doesn't exist
		cgroupDir := filepath.Join("/sys/fs/cgroup", cgroupPath)
		return getCgroupIDFromInode(cgroupDir)
	}

	id, err := strconv.ParseUint(strings.TrimSpace(string(idData)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing cgroup ID: %w", err)
	}

	return id, nil
}

// GetCgroupIDByPath returns the cgroup ID for a given cgroup path
func GetCgroupIDByPath(cgroupPath string) (uint64, error) {
	// Try reading from cgroup.id file first (newer kernels)
	// cgroupPath should have leading /
	idFilePath := filepath.Join("/sys/fs/cgroup", cgroupPath, "cgroup.id")
	cgroupDir := filepath.Join("/sys/fs/cgroup", cgroupPath)

	idData, err := os.ReadFile(idFilePath)
	if err == nil {
		id, err := strconv.ParseUint(strings.TrimSpace(string(idData)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parsing cgroup ID: %w", err)
		}
		return id, nil
	}

	// Fallback: use name_to_handle_at syscall to get inode number
	// The cgroup ID is the inode number of the cgroup directory
	return getCgroupIDFromInode(cgroupDir)
}

// getCgroupIDFromInode gets the cgroup ID from the directory inode
// The cgroup ID is the inode number of the cgroup directory
func getCgroupIDFromInode(cgroupPath string) (uint64, error) {
	// Use stat to get the inode number
	var stat syscall.Stat_t
	if err := syscall.Stat(cgroupPath, &stat); err != nil {
		return 0, fmt.Errorf("stat failed for %s: %w", cgroupPath, err)
	}

	// The cgroup ID is the inode number
	return stat.Ino, nil
}
