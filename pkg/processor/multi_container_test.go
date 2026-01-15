package processor

import (
	"context"
	"fmt"
	"testing"
)

func TestMultiContainerProcessor(t *testing.T) {
	ctx := context.Background()

	// Setup two containers
	containers := map[uint64]*ContainerInfo{
		1000: {CgroupID: 1000, CgroupPath: "/pod/container1", Name: "container1"},
		2000: {CgroupID: 2000, CgroupPath: "/pod/container2", Name: "container2"},
	}

	p := NewProcessor(ctx, containers, nil, 0)

	// Process events from container1
	_, path, result := p.Process(&Event{CgroupID: 1000, PID: 100, Path: "/etc/passwd"})
	if result != ResultNew {
		t.Errorf("container1 first access: got %v, want ResultNew", result)
	}
	if path != "/etc/passwd" {
		t.Errorf("path = %q, want /etc/passwd", path)
	}

	// Process same file from container2 - should be new (per-container dedup)
	_, _, result = p.Process(&Event{CgroupID: 2000, PID: 200, Path: "/etc/passwd"})
	if result != ResultNew {
		t.Errorf("container2 first access: got %v, want ResultNew", result)
	}

	// Process same file from container1 again - should be duplicate
	_, _, result = p.Process(&Event{CgroupID: 1000, PID: 100, Path: "/etc/passwd"})
	if result != ResultDuplicate {
		t.Errorf("container1 second access: got %v, want ResultDuplicate", result)
	}

	// Verify per-container file lists
	files := p.Files()
	if len(files) != 2 {
		t.Fatalf("files map size = %d, want 2", len(files))
	}

	if len(files[1000]) != 1 || files[1000][0] != "/etc/passwd" {
		t.Errorf("container1 files = %v, want [/etc/passwd]", files[1000])
	}
	if len(files[2000]) != 1 || files[2000][0] != "/etc/passwd" {
		t.Errorf("container2 files = %v, want [/etc/passwd]", files[2000])
	}
}

func TestMultiContainerStats(t *testing.T) {
	ctx := context.Background()

	containers := map[uint64]*ContainerInfo{
		1000: {CgroupID: 1000, CgroupPath: "/pod/container1", Name: "container1"},
		2000: {CgroupID: 2000, CgroupPath: "/pod/container2", Name: "container2"},
	}

	p := NewProcessor(ctx, containers, nil, 0)

	// Process events for container1
	p.Process(&Event{CgroupID: 1000, PID: 100, Path: "/etc/passwd"})   // new
	p.Process(&Event{CgroupID: 1000, PID: 100, Path: "/etc/passwd"})   // duplicate
	p.Process(&Event{CgroupID: 1000, PID: 100, Path: "/etc/hostname"}) // new
	p.Process(&Event{CgroupID: 1000, PID: 100, Path: "/proc/self"})    // excluded

	// Process events for container2
	p.Process(&Event{CgroupID: 2000, PID: 200, Path: "/usr/bin/bash"}) // new
	p.Process(&Event{CgroupID: 2000, PID: 200, Path: "/sys/devices"})  // excluded

	stats := p.Stats()

	// Check container1 stats
	c1Stats := stats[1000]
	if c1Stats.EventsReceived != 4 {
		t.Errorf("container1 EventsReceived = %d, want 4", c1Stats.EventsReceived)
	}
	if c1Stats.EventsProcessed != 2 {
		t.Errorf("container1 EventsProcessed = %d, want 2", c1Stats.EventsProcessed)
	}
	if c1Stats.EventsDuplicate != 1 {
		t.Errorf("container1 EventsDuplicate = %d, want 1", c1Stats.EventsDuplicate)
	}
	if c1Stats.EventsExcluded != 1 {
		t.Errorf("container1 EventsExcluded = %d, want 1", c1Stats.EventsExcluded)
	}
	if c1Stats.UniqueFiles != 2 {
		t.Errorf("container1 UniqueFiles = %d, want 2", c1Stats.UniqueFiles)
	}

	// Check container2 stats
	c2Stats := stats[2000]
	if c2Stats.EventsReceived != 2 {
		t.Errorf("container2 EventsReceived = %d, want 2", c2Stats.EventsReceived)
	}
	if c2Stats.EventsProcessed != 1 {
		t.Errorf("container2 EventsProcessed = %d, want 1", c2Stats.EventsProcessed)
	}
	if c2Stats.EventsExcluded != 1 {
		t.Errorf("container2 EventsExcluded = %d, want 1", c2Stats.EventsExcluded)
	}
	if c2Stats.UniqueFiles != 1 {
		t.Errorf("container2 UniqueFiles = %d, want 1", c2Stats.UniqueFiles)
	}

	// Check aggregate stats
	agg := p.Aggregate()
	if agg.EventsReceived != 6 {
		t.Errorf("aggregate EventsReceived = %d, want 6", agg.EventsReceived)
	}
	if agg.EventsProcessed != 3 {
		t.Errorf("aggregate EventsProcessed = %d, want 3", agg.EventsProcessed)
	}
	if agg.UniqueFiles != 3 {
		t.Errorf("aggregate UniqueFiles = %d, want 3", agg.UniqueFiles)
	}
}

func TestUnknownContainer(t *testing.T) {
	ctx := context.Background()

	containers := map[uint64]*ContainerInfo{
		1000: {CgroupID: 1000, CgroupPath: "/pod/container1", Name: "container1"},
	}

	p := NewProcessor(ctx, containers, nil, 0)

	// Process event from unknown container
	cgroupID, path, result := p.Process(&Event{CgroupID: 9999, PID: 100, Path: "/etc/passwd"})
	if result != ResultUnknownContainer {
		t.Errorf("unknown container: got %v, want ResultUnknownContainer", result)
	}
	if cgroupID != 9999 {
		t.Errorf("cgroupID = %d, want 9999", cgroupID)
	}
	if path != "" {
		t.Errorf("path = %q, want empty", path)
	}

	// Check that unknown events are tracked
	agg := p.Aggregate()
	if agg.UnknownEvents != 1 {
		t.Errorf("UnknownEvents = %d, want 1", agg.UnknownEvents)
	}
}

func TestPerContainerDeduplication(t *testing.T) {
	ctx := context.Background()

	containers := map[uint64]*ContainerInfo{
		1000: {CgroupID: 1000, CgroupPath: "/pod/container1", Name: "container1"},
		2000: {CgroupID: 2000, CgroupPath: "/pod/container2", Name: "container2"},
	}

	p := NewProcessor(ctx, containers, nil, 0)

	// Add same file to both containers multiple times
	for i := 0; i < 5; i++ {
		_, _, result := p.Process(&Event{CgroupID: 1000, PID: 100, Path: "/etc/passwd"})
		if i == 0 && result != ResultNew {
			t.Errorf("container1 first access: got %v, want ResultNew", result)
		} else if i > 0 && result != ResultDuplicate {
			t.Errorf("container1 access %d: got %v, want ResultDuplicate", i, result)
		}

		_, _, result = p.Process(&Event{CgroupID: 2000, PID: 200, Path: "/etc/passwd"})
		if i == 0 && result != ResultNew {
			t.Errorf("container2 first access: got %v, want ResultNew", result)
		} else if i > 0 && result != ResultDuplicate {
			t.Errorf("container2 access %d: got %v, want ResultDuplicate", i, result)
		}
	}

	// Both containers should have the file
	files := p.Files()
	if len(files[1000]) != 1 {
		t.Errorf("container1 unique files = %d, want 1", len(files[1000]))
	}
	if len(files[2000]) != 1 {
		t.Errorf("container2 unique files = %d, want 1", len(files[2000]))
	}

	// Both should have same file path
	if files[1000][0] != "/etc/passwd" {
		t.Errorf("container1 file = %q, want /etc/passwd", files[1000][0])
	}
	if files[2000][0] != "/etc/passwd" {
		t.Errorf("container2 file = %q, want /etc/passwd", files[2000][0])
	}
}

func TestPerContainerLRUEviction(t *testing.T) {
	ctx := context.Background()

	containers := map[uint64]*ContainerInfo{
		1000: {CgroupID: 1000, CgroupPath: "/pod/container1", Name: "container1"},
		2000: {CgroupID: 2000, CgroupPath: "/pod/container2", Name: "container2"},
	}

	// Each container has max 3 files
	p := NewProcessor(ctx, containers, []string{}, 3)

	// Add 5 files to container1
	for i := 1; i <= 5; i++ {
		path := fmt.Sprintf("/file%d", i)
		p.Process(&Event{CgroupID: 1000, PID: 100, Path: path})
	}

	// Add 2 files to container2
	for i := 1; i <= 2; i++ {
		path := fmt.Sprintf("/other%d", i)
		p.Process(&Event{CgroupID: 2000, PID: 200, Path: path})
	}

	stats := p.Stats()

	// Container1 should have 3 files (evicted 2)
	c1Stats := stats[1000]
	if c1Stats.UniqueFiles != 3 {
		t.Errorf("container1 UniqueFiles = %d, want 3", c1Stats.UniqueFiles)
	}
	if c1Stats.EventsEvicted != 2 {
		t.Errorf("container1 EventsEvicted = %d, want 2", c1Stats.EventsEvicted)
	}

	// Container2 should have 2 files (no evictions)
	c2Stats := stats[2000]
	if c2Stats.UniqueFiles != 2 {
		t.Errorf("container2 UniqueFiles = %d, want 2", c2Stats.UniqueFiles)
	}
	if c2Stats.EventsEvicted != 0 {
		t.Errorf("container2 EventsEvicted = %d, want 0", c2Stats.EventsEvicted)
	}

	// Aggregate should show 5 unique files and 2 evictions
	agg := p.Aggregate()
	if agg.UniqueFiles != 5 {
		t.Errorf("aggregate UniqueFiles = %d, want 5", agg.UniqueFiles)
	}
	if agg.EventsEvicted != 2 {
		t.Errorf("aggregate EventsEvicted = %d, want 2", agg.EventsEvicted)
	}
}

func TestContainerInfoInStats(t *testing.T) {
	ctx := context.Background()

	containers := map[uint64]*ContainerInfo{
		1000: {CgroupID: 1000, CgroupPath: "/pod/container1", Name: "nginx"},
		2000: {CgroupID: 2000, CgroupPath: "/pod/container2", Name: "sidecar"},
	}

	p := NewProcessor(ctx, containers, nil, 0)

	p.Process(&Event{CgroupID: 1000, PID: 100, Path: "/etc/nginx.conf"})
	p.Process(&Event{CgroupID: 2000, PID: 200, Path: "/etc/fluent.conf"})

	stats := p.Stats()

	// Verify container info is preserved in stats
	c1Stats := stats[1000]
	if c1Stats.Name != "nginx" {
		t.Errorf("container1 Name = %q, want nginx", c1Stats.Name)
	}
	if c1Stats.CgroupPath != "/pod/container1" {
		t.Errorf("container1 CgroupPath = %q, want /pod/container1", c1Stats.CgroupPath)
	}

	c2Stats := stats[2000]
	if c2Stats.Name != "sidecar" {
		t.Errorf("container2 Name = %q, want sidecar", c2Stats.Name)
	}
	if c2Stats.CgroupPath != "/pod/container2" {
		t.Errorf("container2 CgroupPath = %q, want /pod/container2", c2Stats.CgroupPath)
	}
}
