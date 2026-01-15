package reporter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMultiContainerReport(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(ctx, reportPath)

	report := &Report{
		PodName:   "my-app",
		Namespace: "default",
		StartedAt: time.Now().Add(-time.Hour),
		Containers: []ContainerReport{
			{
				Name:        "nginx",
				CgroupID:    1000,
				CgroupPath:  "/pod/nginx",
				Files:       []string{"/etc/nginx/nginx.conf", "/usr/share/nginx/html/index.html"},
				TotalEvents: 50,
				UniqueFiles: 2,
			},
			{
				Name:        "sidecar",
				CgroupID:    2000,
				CgroupPath:  "/pod/sidecar",
				Files:       []string{"/etc/fluent/fluent.conf"},
				TotalEvents: 25,
				UniqueFiles: 1,
			},
		},
		TotalEvents:   75,
		DroppedEvents: 0,
	}

	if err := r.Update(ctx, report); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Read and verify the file
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshaling report: %v", err)
	}

	if got.PodName != "my-app" {
		t.Errorf("PodName = %q, want my-app", got.PodName)
	}
	if got.Namespace != "default" {
		t.Errorf("Namespace = %q, want default", got.Namespace)
	}
	if len(got.Containers) != 2 {
		t.Fatalf("len(Containers) = %d, want 2", len(got.Containers))
	}
	if got.TotalEvents != 75 {
		t.Errorf("TotalEvents = %d, want 75", got.TotalEvents)
	}

	// Check first container
	c1 := got.Containers[0]
	if c1.Name != "nginx" {
		t.Errorf("Container[0].Name = %q, want nginx", c1.Name)
	}
	if c1.CgroupID != 1000 {
		t.Errorf("Container[0].CgroupID = %d, want 1000", c1.CgroupID)
	}
	if len(c1.Files) != 2 {
		t.Errorf("Container[0] files = %d, want 2", len(c1.Files))
	}

	// Check second container
	c2 := got.Containers[1]
	if c2.Name != "sidecar" {
		t.Errorf("Container[1].Name = %q, want sidecar", c2.Name)
	}
	if c2.CgroupID != 2000 {
		t.Errorf("Container[1].CgroupID = %d, want 2000", c2.CgroupID)
	}
	if len(c2.Files) != 1 {
		t.Errorf("Container[1] files = %d, want 1", len(c2.Files))
	}
}

func TestReportSortsContainers(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(ctx, reportPath)

	// Containers in reverse order by cgroup ID
	report := &Report{
		StartedAt: time.Now(),
		Containers: []ContainerReport{
			{Name: "third", CgroupID: 3000, CgroupPath: "/c3", Files: []string{"/file3"}},
			{Name: "first", CgroupID: 1000, CgroupPath: "/c1", Files: []string{"/file1"}},
			{Name: "second", CgroupID: 2000, CgroupPath: "/c2", Files: []string{"/file2"}},
		},
		TotalEvents: 3,
	}

	if err := r.Update(ctx, report); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshaling report: %v", err)
	}

	// Should be sorted by cgroup ID
	if got.Containers[0].CgroupID != 1000 {
		t.Errorf("Containers[0].CgroupID = %d, want 1000", got.Containers[0].CgroupID)
	}
	if got.Containers[1].CgroupID != 2000 {
		t.Errorf("Containers[1].CgroupID = %d, want 2000", got.Containers[1].CgroupID)
	}
	if got.Containers[2].CgroupID != 3000 {
		t.Errorf("Containers[2].CgroupID = %d, want 3000", got.Containers[2].CgroupID)
	}
}

func TestReportSortsFilesPerContainer(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(ctx, reportPath)

	report := &Report{
		StartedAt: time.Now(),
		Containers: []ContainerReport{
			{
				Name:       "app",
				CgroupID:   1000,
				CgroupPath: "/pod/app",
				// Files in unsorted order
				Files:       []string{"/z/last", "/a/first", "/m/middle"},
				TotalEvents: 3,
				UniqueFiles: 3,
			},
		},
		TotalEvents: 3,
	}

	if err := r.Update(ctx, report); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshaling report: %v", err)
	}

	files := got.Containers[0].Files
	expected := []string{"/a/first", "/m/middle", "/z/last"}
	for i, f := range files {
		if f != expected[i] {
			t.Errorf("Files[%d] = %q, want %q", i, f, expected[i])
		}
	}
}

func TestContainerReportJSONFields(t *testing.T) {
	report := &Report{
		PodName:     "test-pod",
		Namespace:   "default",
		StartedAt:   time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		Containers:  []ContainerReport{{Name: "app", CgroupID: 1000, CgroupPath: "/pod/app", Files: []string{"/test"}, TotalEvents: 1, UniqueFiles: 1}},
		TotalEvents: 1,
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Check top-level fields
	expectedTopLevel := []string{"pod_name", "namespace", "started_at", "last_updated_at", "containers", "total_events", "dropped_events"}
	for _, field := range expectedTopLevel {
		if _, ok := raw[field]; !ok {
			t.Errorf("expected top-level field %q not found", field)
		}
	}

	// Check container fields
	containers, ok := raw["containers"].([]any)
	if !ok || len(containers) == 0 {
		t.Fatal("containers field missing or empty")
	}

	container := containers[0].(map[string]any)
	expectedContainerFields := []string{"name", "cgroup_id", "cgroup_path", "files", "total_events", "unique_files"}
	for _, field := range expectedContainerFields {
		if _, ok := container[field]; !ok {
			t.Errorf("expected container field %q not found", field)
		}
	}
}

func TestEmptyContainersList(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(ctx, reportPath)

	report := &Report{
		StartedAt:   time.Now(),
		Containers:  []ContainerReport{},
		TotalEvents: 0,
	}

	if err := r.Update(ctx, report); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshaling report: %v", err)
	}

	if got.Containers == nil {
		t.Error("Containers should not be nil")
	}
	if len(got.Containers) != 0 {
		t.Errorf("len(Containers) = %d, want 0", len(got.Containers))
	}
}

func TestContainerWithNoFiles(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(ctx, reportPath)

	report := &Report{
		StartedAt: time.Now(),
		Containers: []ContainerReport{
			{
				Name:        "app",
				CgroupID:    1000,
				CgroupPath:  "/pod/app",
				Files:       []string{}, // No files yet
				TotalEvents: 10,
				UniqueFiles: 0,
			},
		},
		TotalEvents: 10,
	}

	if err := r.Update(ctx, report); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshaling report: %v", err)
	}

	if len(got.Containers) != 1 {
		t.Fatalf("len(Containers) = %d, want 1", len(got.Containers))
	}

	if got.Containers[0].Files == nil {
		t.Error("Files should not be nil")
	}
	if len(got.Containers[0].Files) != 0 {
		t.Errorf("len(Files) = %d, want 0", len(got.Containers[0].Files))
	}
	if got.Containers[0].UniqueFiles != 0 {
		t.Errorf("UniqueFiles = %d, want 0", got.Containers[0].UniqueFiles)
	}
}

func TestMultipleUpdates(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(ctx, reportPath)

	// First update
	report := &Report{
		PodName:   "my-app",
		StartedAt: time.Now(),
		Containers: []ContainerReport{
			{Name: "app", CgroupID: 1000, CgroupPath: "/pod/app", Files: []string{"/file1"}, TotalEvents: 1, UniqueFiles: 1},
		},
		TotalEvents: 1,
	}

	if err := r.Update(ctx, report); err != nil {
		t.Fatalf("First update failed: %v", err)
	}

	// Second update with more data
	report.Containers[0].Files = []string{"/file1", "/file2", "/file3"}
	report.Containers[0].TotalEvents = 10
	report.Containers[0].UniqueFiles = 3
	report.TotalEvents = 10

	if err := r.Update(ctx, report); err != nil {
		t.Fatalf("Second update failed: %v", err)
	}

	// Verify latest state
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("reading report file: %v", err)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshaling report: %v", err)
	}

	if len(got.Containers[0].Files) != 3 {
		t.Errorf("Files count = %d, want 3", len(got.Containers[0].Files))
	}
	if got.Containers[0].TotalEvents != 10 {
		t.Errorf("TotalEvents = %d, want 10", got.Containers[0].TotalEvents)
	}
}
