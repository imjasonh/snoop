package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MultiContainerReport matches the new JSON structure from snoop
type MultiContainerReport struct {
	PodName       string            `json:"pod_name"`
	Namespace     string            `json:"namespace"`
	StartedAt     time.Time         `json:"started_at"`
	LastUpdatedAt time.Time         `json:"last_updated_at"`
	Containers    []ContainerReport `json:"containers"`
	TotalEvents   uint64            `json:"total_events"`
	DroppedEvents uint64            `json:"dropped_events"`
}

type ContainerReport struct {
	Name        string   `json:"name"`
	CgroupID    uint64   `json:"cgroup_id"`
	CgroupPath  string   `json:"cgroup_path"`
	Files       []string `json:"files"`
	TotalEvents uint64   `json:"total_events"`
	UniqueFiles int      `json:"unique_files"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <report.json>\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}

	reportPath := os.Args[1]

	fmt.Printf("Validating multi-container report: %s\n", reportPath)

	if err := validateMultiContainerReport(reportPath); err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n✅ Multi-container report validation passed")
}

func validateMultiContainerReport(path string) error {
	// Read and parse JSON
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading report: %w", err)
	}

	var report MultiContainerReport
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

	// Validate containers array
	if len(report.Containers) == 0 {
		return fmt.Errorf("containers array is empty")
	}
	fmt.Printf("✓ Containers Found: %d\n", len(report.Containers))

	// Validate timestamps
	if report.LastUpdatedAt.Before(report.StartedAt) {
		return fmt.Errorf("last_updated_at (%s) is before started_at (%s)",
			report.LastUpdatedAt.Format(time.RFC3339),
			report.StartedAt.Format(time.RFC3339))
	}
	fmt.Println("✓ Timestamps are consistent")

	// Validate each container
	fmt.Println("\n=== Container Details ===")
	totalFiles := 0
	containersWithFiles := 0

	for i, container := range report.Containers {
		fmt.Printf("\nContainer %d: %s\n", i+1, container.Name)

		if container.Name == "" {
			return fmt.Errorf("container %d has empty name", i+1)
		}

		if container.CgroupID == 0 {
			return fmt.Errorf("container %s has zero cgroup_id", container.Name)
		}
		fmt.Printf("  ✓ Cgroup ID: %d\n", container.CgroupID)

		if container.CgroupPath == "" {
			return fmt.Errorf("container %s has empty cgroup_path", container.Name)
		}
		fmt.Printf("  ✓ Cgroup Path: %s\n", container.CgroupPath)

		fmt.Printf("  ✓ Files: %d\n", len(container.Files))
		fmt.Printf("  ✓ Total Events: %d\n", container.TotalEvents)
		fmt.Printf("  ✓ Unique Files: %d\n", container.UniqueFiles)

		if len(container.Files) != container.UniqueFiles {
			return fmt.Errorf("container %s: files array length (%d) != unique_files (%d)",
				container.Name, len(container.Files), container.UniqueFiles)
		}

		// Validate files in this container
		if len(container.Files) > 0 {
			if err := validateContainerFiles(container); err != nil {
				return fmt.Errorf("container %s: %w", container.Name, err)
			}
			containersWithFiles++
		}

		totalFiles += len(container.Files)

		// Show sample files
		if len(container.Files) > 0 && len(container.Files) <= 5 {
			fmt.Printf("  Files:\n")
			for _, file := range container.Files {
				fmt.Printf("    - %s\n", file)
			}
		} else if len(container.Files) > 5 {
			fmt.Printf("  Sample files (first 5):\n")
			for j := 0; j < 5; j++ {
				fmt.Printf("    - %s\n", container.Files[j])
			}
			fmt.Printf("    ... and %d more\n", len(container.Files)-5)
		}
	}

	// Validate aggregate stats
	fmt.Println("\n=== Aggregate Statistics ===")
	fmt.Printf("Total Containers: %d\n", len(report.Containers))
	fmt.Printf("Containers with Files: %d\n", containersWithFiles)
	fmt.Printf("Total Events: %d\n", report.TotalEvents)
	fmt.Printf("Dropped Events: %d\n", report.DroppedEvents)
	fmt.Printf("Total Files (all containers): %d\n", totalFiles)

	if report.DroppedEvents > 0 {
		dropRate := float64(report.DroppedEvents) / float64(report.TotalEvents) * 100
		fmt.Printf("Drop Rate: %.2f%%\n", dropRate)
		if dropRate > 10.0 {
			return fmt.Errorf("drop rate too high: %.2f%% (should be < 10%%)", dropRate)
		}
	}

	// Require at least one container with files
	if containersWithFiles == 0 {
		return fmt.Errorf("no containers have any files captured")
	}
	fmt.Printf("✓ At least one container has files captured\n")

	return nil
}

func validateContainerFiles(container ContainerReport) error {
	excludedPrefixes := []string{"/proc/", "/sys/", "/dev/"}

	// Check for excluded paths
	for _, file := range container.Files {
		for _, prefix := range excludedPrefixes {
			if strings.HasPrefix(file, prefix) {
				return fmt.Errorf("excluded file found: %s", file)
			}
		}
	}

	// Check paths are absolute
	for _, file := range container.Files {
		if !strings.HasPrefix(file, "/") {
			return fmt.Errorf("relative path found: %s", file)
		}
	}

	// Check for path components that should be normalized
	for _, file := range container.Files {
		if strings.Contains(file, "/./") || strings.Contains(file, "/../") {
			return fmt.Errorf("non-normalized path: %s", file)
		}
	}

	// Check for duplicates within this container
	seen := make(map[string]bool)
	for _, file := range container.Files {
		if seen[file] {
			return fmt.Errorf("duplicate file: %s", file)
		}
		seen[file] = true
	}

	return nil
}
