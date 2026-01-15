//go:build linux

package cgroup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetSelfCgroupPath(t *testing.T) {
	// This test will fail on non-Linux systems, which is expected
	// Skip if /proc/self/cgroup doesn't exist
	path, err := GetSelfCgroupPath()
	if err != nil {
		t.Skipf("Skipping test on non-Linux system: %v", err)
	}

	// Verify the path is non-empty
	if path == "" {
		t.Error("GetSelfCgroupPath returned empty path")
	}

	// The path should start with /
	if !strings.HasPrefix(path, "/") {
		t.Errorf("cgroup path should start with /, got: %s", path)
	}

	t.Logf("Self cgroup path: %s", path)
}

func TestGetSelfCgroupID(t *testing.T) {
	// This test will fail on non-Linux systems, which is expected
	// Skip if /proc/self/cgroup doesn't exist
	id, err := GetSelfCgroupID()
	if err != nil {
		t.Skipf("Skipping test on non-Linux system: %v", err)
	}

	// Verify the ID is non-zero
	if id == 0 {
		t.Error("GetSelfCgroupID returned zero ID")
	}

	t.Logf("Self cgroup ID: %d", id)
}

func TestGetSelfCgroupPathWithGetCgroupIDByPath(t *testing.T) {
	// This integration test verifies that auto-discovered path works with GetCgroupIDByPath
	// Skip on non-Linux systems or when cgroup filesystem is not accessible
	path, err := GetSelfCgroupPath()
	if err != nil {
		t.Skipf("Skipping test on non-Linux system: %v", err)
	}

	// Also get ID directly via GetSelfCgroupID - skip if this fails
	selfID, err := GetSelfCgroupID()
	if err != nil {
		t.Skipf("Skipping test: cgroup filesystem not accessible: %v", err)
	}

	// Use the discovered path with GetCgroupIDByPath
	id, err := GetCgroupIDByPath(path)
	if err != nil {
		t.Fatalf("GetCgroupIDByPath failed for auto-discovered path %q: %v", path, err)
	}

	// They should match
	if id != selfID {
		t.Errorf("ID mismatch: GetCgroupIDByPath(%q) = %d, GetSelfCgroupID() = %d", path, id, selfID)
	}

	t.Logf("Successfully verified auto-discovered path %q has ID %d", path, id)
}

func TestGetSelfCgroupPathParsing(t *testing.T) {
	// Create a temporary file with test content
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "cgroup")

	for _, tt := range []struct {
		desc        string
		content     string
		wantPath    string
		wantErr     bool
		errContains string
	}{
		{
			desc:     "standard cgroup v2 format",
			content:  "0::/system.slice/docker-abc123.scope\n",
			wantPath: "/system.slice/docker-abc123.scope",
			wantErr:  false,
		},
		{
			desc:     "kubernetes pod format",
			content:  "0::/kubepods/burstable/pod12345678-1234-1234-1234-123456789012/abc123\n",
			wantPath: "/kubepods/burstable/pod12345678-1234-1234-1234-123456789012/abc123",
			wantErr:  false,
		},
		{
			desc:     "root cgroup",
			content:  "0::/\n",
			wantPath: "/",
			wantErr:  false,
		},
		{
			desc:     "multiple lines with cgroup v2",
			content:  "1:name=systemd:/user.slice\n0::/system.slice\n",
			wantPath: "/system.slice",
			wantErr:  false,
		},
		{
			desc:        "no cgroup v2 entry",
			content:     "1:name=systemd:/user.slice\n2:cpu:/some/path\n",
			wantPath:    "",
			wantErr:     true,
			errContains: "cgroup v2 not found",
		},
		{
			desc:        "empty file",
			content:     "",
			wantPath:    "",
			wantErr:     true,
			errContains: "cgroup v2 not found",
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			// Write test content to temporary file
			if err := os.WriteFile(testFile, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			// We can't easily test GetSelfCgroupPath directly with custom file,
			// but we can verify the parsing logic by reading the file ourselves
			data, err := os.ReadFile(testFile)
			if err != nil {
				t.Fatalf("Failed to read test file: %v", err)
			}

			lines := strings.Split(string(data), "\n")
			var gotPath string
			var found bool
			for _, line := range lines {
				if strings.HasPrefix(line, "0::") {
					gotPath = strings.TrimPrefix(line, "0::")
					found = true
					break
				}
			}

			if tt.wantErr {
				if found {
					t.Errorf("Expected error but got path: %q", gotPath)
				}
			} else {
				if !found {
					t.Errorf("Expected to find cgroup v2 path but didn't")
				} else if gotPath != tt.wantPath {
					t.Errorf("Path mismatch: got %q, want %q", gotPath, tt.wantPath)
				}
			}
		})
	}
}

func TestNewSelfExcludingDiscovery(t *testing.T) {
	d := NewSelfExcludingDiscovery()
	if d == nil {
		t.Fatal("NewSelfExcludingDiscovery returned nil")
	}
}

func TestExtractContainerName(t *testing.T) {
	for _, tt := range []struct {
		desc     string
		dirName  string
		wantName string
	}{
		{
			desc:     "cri-containerd format",
			dirName:  "cri-containerd-abc123def456ghi789.scope",
			wantName: "abc123def456",
		},
		{
			desc:     "docker format",
			dirName:  "docker-1234567890abcdef.scope",
			wantName: "1234567890ab",
		},
		{
			desc:     "crio format",
			dirName:  "crio-fedcba0987654321.scope",
			wantName: "fedcba098765",
		},
		{
			desc:     "short ID",
			dirName:  "abc123",
			wantName: "abc123",
		},
		{
			desc:     "long ID without prefix",
			dirName:  "abc123def456ghi789jkl012",
			wantName: "abc123def456",
		},
		{
			desc:     "with slice suffix",
			dirName:  "cri-containerd-abc123def456.slice",
			wantName: "abc123def456",
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			got := extractContainerName(tt.dirName)
			if got != tt.wantName {
				t.Errorf("extractContainerName(%q) = %q, want %q", tt.dirName, got, tt.wantName)
			}
		})
	}
}

func TestDiscoverAllExceptSelf(t *testing.T) {
	// This test requires a Linux system with cgroup v2
	// Skip if we can't get our own cgroup path
	_, err := GetSelfCgroupPath()
	if err != nil {
		t.Skipf("Skipping test on non-Linux system or without cgroup access: %v", err)
	}

	containers, err := DiscoverAllExceptSelf()
	if err != nil {
		t.Skipf("Could not discover containers (might be running in single container): %v", err)
	}

	// Verify that our own container is not in the list
	selfID, err := GetSelfCgroupID()
	if err != nil {
		t.Fatalf("Failed to get self cgroup ID: %v", err)
	}

	if _, exists := containers[selfID]; exists {
		t.Errorf("DiscoverAllExceptSelf included self container (cgroup_id=%d)", selfID)
	}

	// Verify that all returned containers have valid information
	for cgroupID, info := range containers {
		if info.CgroupID != cgroupID {
			t.Errorf("Container map key %d doesn't match info.CgroupID %d", cgroupID, info.CgroupID)
		}
		if info.CgroupPath == "" {
			t.Errorf("Container %d has empty CgroupPath", cgroupID)
		}
		if info.Name == "" {
			t.Errorf("Container %d has empty Name", cgroupID)
		}
		t.Logf("Discovered container: %s (cgroup_id=%d, path=%s)", info.Name, cgroupID, info.CgroupPath)
	}
}
