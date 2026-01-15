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
)

// ContainerInfo holds information about a discovered container.
type ContainerInfo struct {
	CgroupID   uint64
	CgroupPath string
	Name       string // Short container ID or name
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

		containers[cgroupID] = &ContainerInfo{
			CgroupID:   cgroupID,
			CgroupPath: containerCgroupPath,
			Name:       shortName,
		}
	}

	return containers, nil
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
