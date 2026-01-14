package reporter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileReporterUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(reportPath)

	report := &Report{
		ContainerID: "abc123",
		ImageRef:    "nginx:latest",
		StartedAt:   time.Now().Add(-time.Hour),
		Files:       []string{"/etc/passwd", "/usr/bin/bash", "/lib/libc.so.6"},
		TotalEvents: 100,
	}

	if err := r.Update(context.Background(), report); err != nil {
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

	if got.ContainerID != "abc123" {
		t.Errorf("ContainerID = %q, want %q", got.ContainerID, "abc123")
	}
	if got.ImageRef != "nginx:latest" {
		t.Errorf("ImageRef = %q, want %q", got.ImageRef, "nginx:latest")
	}
	if got.TotalEvents != 100 {
		t.Errorf("TotalEvents = %d, want 100", got.TotalEvents)
	}
	if len(got.Files) != 3 {
		t.Errorf("len(Files) = %d, want 3", len(got.Files))
	}
}

func TestFileReporterSortsFiles(t *testing.T) {
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(reportPath)

	// Files in unsorted order
	report := &Report{
		StartedAt: time.Now(),
		Files:     []string{"/z/last", "/a/first", "/m/middle"},
	}

	if err := r.Update(context.Background(), report); err != nil {
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

	// Should be sorted
	expected := []string{"/a/first", "/m/middle", "/z/last"}
	for i, f := range got.Files {
		if f != expected[i] {
			t.Errorf("Files[%d] = %q, want %q", i, f, expected[i])
		}
	}
}

func TestFileReporterCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	// Nested path that doesn't exist
	reportPath := filepath.Join(tmpDir, "nested", "dir", "report.json")

	r := NewFileReporter(reportPath)

	report := &Report{
		StartedAt: time.Now(),
		Files:     []string{"/etc/passwd"},
	}

	if err := r.Update(context.Background(), report); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if _, err := os.Stat(reportPath); err != nil {
		t.Errorf("report file should exist: %v", err)
	}
}

func TestFileReporterAtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(reportPath)

	// Write initial report
	report := &Report{
		StartedAt: time.Now(),
		Files:     []string{"/initial"},
	}
	if err := r.Update(context.Background(), report); err != nil {
		t.Fatalf("first Update failed: %v", err)
	}

	// Write updated report
	report.Files = []string{"/updated", "/more"}
	if err := r.Update(context.Background(), report); err != nil {
		t.Fatalf("second Update failed: %v", err)
	}

	// Verify no temp files left behind
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("reading dir: %v", err)
	}

	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}

	// Verify content is updated
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("reading report: %v", err)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}

	if len(got.Files) != 2 {
		t.Errorf("len(Files) = %d, want 2", len(got.Files))
	}
}

func TestFileReporterSetsLastUpdatedAt(t *testing.T) {
	tmpDir := t.TempDir()
	reportPath := filepath.Join(tmpDir, "report.json")

	r := NewFileReporter(reportPath)

	before := time.Now()
	report := &Report{
		StartedAt: time.Now().Add(-time.Hour),
		Files:     []string{"/test"},
	}

	if err := r.Update(context.Background(), report); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	after := time.Now()

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("reading report: %v", err)
	}

	var got Report
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}

	if got.LastUpdatedAt.Before(before) || got.LastUpdatedAt.After(after) {
		t.Errorf("LastUpdatedAt = %v, expected between %v and %v",
			got.LastUpdatedAt, before, after)
	}
}

func TestFileReporterPath(t *testing.T) {
	r := NewFileReporter("/data/report.json")
	if r.Path() != "/data/report.json" {
		t.Errorf("Path() = %q, want %q", r.Path(), "/data/report.json")
	}
}

func TestFileReporterClose(t *testing.T) {
	r := NewFileReporter("/tmp/report.json")
	if err := r.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
}

func TestReportJSONFields(t *testing.T) {
	report := &Report{
		ContainerID:   "container-123",
		ImageRef:      "myimage:v1",
		ImageDigest:   "sha256:abc123",
		PodName:       "my-pod",
		Namespace:     "default",
		Labels:        map[string]string{"app": "test"},
		StartedAt:     time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
		LastUpdatedAt: time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
		Files:         []string{"/etc/passwd"},
		TotalEvents:   1000,
		DroppedEvents: 5,
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify JSON field names
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal to map failed: %v", err)
	}

	expectedFields := []string{
		"container_id", "image_ref", "image_digest", "pod_name",
		"namespace", "labels", "started_at", "last_updated_at",
		"files", "total_events", "dropped_events",
	}

	for _, field := range expectedFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("expected JSON field %q not found", field)
		}
	}
}

func TestReportOmitsEmptyFields(t *testing.T) {
	report := &Report{
		StartedAt: time.Now(),
		Files:     []string{"/test"},
	}

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// These should be omitted when empty
	omittedWhenEmpty := []string{
		"container_id", "image_ref", "image_digest",
		"pod_name", "namespace", "labels",
	}

	for _, field := range omittedWhenEmpty {
		if _, ok := raw[field]; ok {
			t.Errorf("field %q should be omitted when empty", field)
		}
	}
}
