package reporter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/chainguard-dev/clog"
)

// Report represents the file access report for a pod with multiple containers.
type Report struct {
	// Pod-level metadata
	PodName   string `json:"pod_name,omitempty"`
	Namespace string `json:"namespace,omitempty"`

	// Timing
	StartedAt     time.Time `json:"started_at"`
	LastUpdatedAt time.Time `json:"last_updated_at"`

	// Per-container data
	Containers []ContainerReport `json:"containers"`

	// Aggregate stats
	TotalEvents   uint64 `json:"total_events"`
	DroppedEvents uint64 `json:"dropped_events"`
}

// ContainerReport represents the file access report for a single container.
type ContainerReport struct {
	Name        string   `json:"name"`
	CgroupID    uint64   `json:"cgroup_id"`
	CgroupPath  string   `json:"cgroup_path"`
	Files       []string `json:"files"`
	TotalEvents uint64   `json:"total_events"`
	UniqueFiles int      `json:"unique_files"`
}

// Reporter defines the interface for report output.
type Reporter interface {
	// Update writes the current report state.
	Update(ctx context.Context, report *Report) error

	// Close flushes any pending data and releases resources.
	Close() error
}

// FileReporter writes reports to a JSON file using atomic writes.
type FileReporter struct {
	ctx  context.Context
	path string
}

// NewFileReporter creates a reporter that writes to the given file path.
// The file is written atomically using a temp file + rename.
func NewFileReporter(ctx context.Context, path string) *FileReporter {
	log := clog.FromContext(ctx)
	log.Infof("Initialized file reporter (path: %s)", path)
	return &FileReporter{
		ctx:  ctx,
		path: path,
	}
}

// Update writes the report to the file atomically.
func (r *FileReporter) Update(ctx context.Context, report *Report) error {
	log := clog.FromContext(ctx)

	// Make a copy and ensure files are sorted within each container
	reportCopy := *report
	reportCopy.Containers = make([]ContainerReport, len(report.Containers))
	copy(reportCopy.Containers, report.Containers)

	// Sort containers by cgroup ID for consistent ordering
	sort.Slice(reportCopy.Containers, func(i, j int) bool {
		return reportCopy.Containers[i].CgroupID < reportCopy.Containers[j].CgroupID
	})

	// Ensure each container's files are sorted
	totalFiles := 0
	for i := range reportCopy.Containers {
		// Files should already be sorted from processor, but ensure it
		sort.Strings(reportCopy.Containers[i].Files)
		totalFiles += len(reportCopy.Containers[i].Files)
	}

	reportCopy.LastUpdatedAt = time.Now()

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(&reportCopy, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	log.Debugf("Marshaled report: %d bytes, %d containers, %d total files", len(data), len(reportCopy.Containers), totalFiles)

	// Write atomically: write to temp file, then rename
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".snoop-report-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	log.Debugf("Writing report to temporary file: %s", tmpPath)

	// Clean up temp file on error
	defer func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, r.path); err != nil {
		return fmt.Errorf("renaming temp file to %s: %w", r.path, err)
	}

	tmpPath = "" // Prevent cleanup since rename succeeded
	log.Debug("Report written successfully")
	return nil
}

// Close is a no-op for FileReporter.
func (r *FileReporter) Close() error {
	return nil
}

// Path returns the file path this reporter writes to.
func (r *FileReporter) Path() string {
	return r.path
}
