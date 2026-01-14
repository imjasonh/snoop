package cgroup

import (
	"testing"
)

func TestGetSelfCgroupID(t *testing.T) {
	// This test will fail on non-Linux systems, which is expected
	// Skip if /proc/self/cgroup doesn't exist
	_, err := GetSelfCgroupID()
	if err != nil {
		t.Skipf("Skipping test on non-Linux system: %v", err)
	}
}

func TestNewSelfExcludingDiscovery(t *testing.T) {
	d := NewSelfExcludingDiscovery()
	if d == nil {
		t.Fatal("NewSelfExcludingDiscovery returned nil")
	}
}
