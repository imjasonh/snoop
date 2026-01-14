package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Report matches the JSON structure from snoop
type Report struct {
	ContainerID   string            `json:"container_id"`
	ImageRef      string            `json:"image_ref"`
	ImageDigest   string            `json:"image_digest"`
	PodName       string            `json:"pod_name"`
	Namespace     string            `json:"namespace"`
	Labels        map[string]string `json:"labels"`
	StartedAt     time.Time         `json:"started_at"`
	LastUpdatedAt time.Time         `json:"last_updated_at"`
	Files         []string          `json:"files"`
	TotalEvents   uint64            `json:"total_events"`
	DroppedEvents uint64            `json:"dropped_events"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <report.json>\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}

	reportPath := os.Args[1]

	fmt.Printf("Validating report: %s\n", reportPath)

	if err := validateReport(reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✅ Report validation passed")
}

func validateReport(path string) error {
	// Read and parse JSON
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading report: %w", err)
	}

	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}

	fmt.Println("\n=== Report Structure ===")

	// Validate required fields
	if report.PodName == "" {
		return fmt.Errorf("pod_name is empty")
	}
	fmt.Printf("✓ Pod Name: %s\n", report.PodName)

	if report.Namespace == "" {
		return fmt.Errorf("namespace is empty")
	}
	fmt.Printf("✓ Namespace: %s\n", report.Namespace)

	if report.StartedAt.IsZero() {
		return fmt.Errorf("started_at is zero")
	}
	fmt.Printf("✓ Started At: %s\n", report.StartedAt.Format(time.RFC3339))

	if report.LastUpdatedAt.IsZero() {
		return fmt.Errorf("last_updated_at is zero")
	}
	fmt.Printf("✓ Last Updated: %s\n", report.LastUpdatedAt.Format(time.RFC3339))

	// Validate files array
	if len(report.Files) == 0 {
		return fmt.Errorf("files array is empty")
	}
	fmt.Printf("✓ Files Captured: %d\n", len(report.Files))

	fmt.Println("\n=== File Validation ===")

	// Check for excluded paths
	excludedPrefixes := []string{"/proc/", "/sys/", "/dev/"}
	excludedCount := 0
	for _, file := range report.Files {
		for _, prefix := range excludedPrefixes {
			if strings.HasPrefix(file, prefix) {
				excludedCount++
				if excludedCount <= 3 {
					fmt.Printf("⚠ Excluded file found: %s\n", file)
				}
			}
		}
	}

	if excludedCount > 0 {
		return fmt.Errorf("found %d excluded files (should be 0)", excludedCount)
	}
	fmt.Println("✓ No excluded files (/proc, /sys, /dev)")

	// Check paths are absolute
	relativeCount := 0
	for _, file := range report.Files {
		if !strings.HasPrefix(file, "/") {
			relativeCount++
			if relativeCount <= 3 {
				fmt.Printf("⚠ Relative path found: %s\n", file)
			}
		}
	}

	if relativeCount > 0 {
		return fmt.Errorf("found %d relative paths (should be 0)", relativeCount)
	}
	fmt.Println("✓ All paths are absolute")

	// Check for path components that should be normalized
	unnormalizedCount := 0
	for _, file := range report.Files {
		if strings.Contains(file, "/./") || strings.Contains(file, "/../") {
			unnormalizedCount++
			if unnormalizedCount <= 3 {
				fmt.Printf("⚠ Non-normalized path: %s\n", file)
			}
		}
	}

	if unnormalizedCount > 0 {
		return fmt.Errorf("found %d non-normalized paths", unnormalizedCount)
	}
	fmt.Println("✓ All paths are normalized")

	// Check for duplicates
	seen := make(map[string]bool)
	duplicates := 0
	for _, file := range report.Files {
		if seen[file] {
			duplicates++
			if duplicates <= 3 {
				fmt.Printf("⚠ Duplicate file: %s\n", file)
			}
		}
		seen[file] = true
	}

	if duplicates > 0 {
		return fmt.Errorf("found %d duplicate files", duplicates)
	}
	fmt.Println("✓ No duplicate files")

	// Validate timestamps
	if report.LastUpdatedAt.Before(report.StartedAt) {
		return fmt.Errorf("last_updated_at (%s) is before started_at (%s)",
			report.LastUpdatedAt.Format(time.RFC3339),
			report.StartedAt.Format(time.RFC3339))
	}
	fmt.Println("✓ Timestamps are consistent")

	// Statistics
	fmt.Println("\n=== Statistics ===")
	fmt.Printf("Total Events: %d\n", report.TotalEvents)
	fmt.Printf("Dropped Events: %d\n", report.DroppedEvents)
	fmt.Printf("Unique Files: %d\n", len(report.Files))

	if report.DroppedEvents > 0 {
		dropRate := float64(report.DroppedEvents) / float64(report.TotalEvents) * 100
		fmt.Printf("Drop Rate: %.2f%%\n", dropRate)
		if dropRate > 5.0 {
			return fmt.Errorf("drop rate too high: %.2f%% (should be < 5%%)", dropRate)
		}
	}

	// Show sample files
	fmt.Println("\n=== Sample Files (first 10) ===")
	for i, file := range report.Files {
		if i >= 10 {
			fmt.Printf("... and %d more\n", len(report.Files)-10)
			break
		}
		fmt.Printf("  %s\n", file)
	}

	return nil
}
