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

// Report represents the file access report for a container.
type Report struct {
	// Identity
	ContainerID string            `json:"container_id,omitempty"`
	ImageRef    string            `json:"image_ref,omitempty"`
	ImageDigest string            `json:"image_digest,omitempty"`
	PodName     string            `json:"pod_name,omitempty"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`

	// Timing
	StartedAt     time.Time `json:"started_at"`
	LastUpdatedAt time.Time `json:"last_updated_at"`

	// Data
	Files []string `json:"files"`

	// Stats
	TotalEvents   uint64 `json:"total_events"`
	DroppedEvents uint64 `json:"dropped_events"`
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

	// Sort files for consistent output
	sortedFiles := make([]string, len(report.Files))
	copy(sortedFiles, report.Files)
	sort.Strings(sortedFiles)

	reportCopy := *report
	reportCopy.Files = sortedFiles
	reportCopy.LastUpdatedAt = time.Now()

	// Marshal to JSON with indentation for readability
	data, err := json.MarshalIndent(&reportCopy, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling report: %w", err)
	}

	log.Debugf("Marshaled report: %d bytes, %d files", len(data), len(sortedFiles))

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
