package cgroup

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

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

// GetSelfCgroupID returns the cgroup ID of the current process
func GetSelfCgroupID() (uint64, error) {
	// Read /proc/self/cgroup to get cgroup path
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return 0, fmt.Errorf("reading /proc/self/cgroup: %w", err)
	}

	// Parse cgroup v2 format: 0::/path/to/cgroup
	lines := strings.Split(string(data), "\n")
	var cgroupPath string
	for _, line := range lines {
		if strings.HasPrefix(line, "0::") {
			cgroupPath = strings.TrimPrefix(line, "0::")
			break
		}
	}

	if cgroupPath == "" {
		return 0, fmt.Errorf("cgroup v2 not found in /proc/self/cgroup")
	}

	// Read the cgroup.id file to get the cgroup ID
	// The path is /sys/fs/cgroup/<cgroup_path>/cgroup.id
	// For root cgroup, it's just /sys/fs/cgroup/cgroup.id
	idPath := "/sys/fs/cgroup" + cgroupPath
	if !strings.HasSuffix(idPath, "/") {
		idPath += "/"
	}
	idPath += "cgroup.id"

	idData, err := os.ReadFile(idPath)
	if err != nil {
		return 0, fmt.Errorf("reading cgroup.id from %s: %w", idPath, err)
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
	idPath := "/sys/fs/cgroup" + cgroupPath
	if !strings.HasSuffix(idPath, "/") {
		idPath += "/"
	}
	idFilePath := idPath + "cgroup.id"

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
	return getCgroupIDFromInode(strings.TrimSuffix(idPath, "/"))
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
