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
	Name        string             `json:"name"`
	CgroupID    uint64             `json:"cgroup_id"`
	CgroupPath  string             `json:"cgroup_path"`
	Files       []string           `json:"files"`
	TotalEvents uint64             `json:"total_events"`
	UniqueFiles int                `json:"unique_files"`
	APKPackages []APKPackageReport `json:"apk_packages,omitempty"`
}

type APKPackageReport struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	TotalFiles    int    `json:"total_files"`
	AccessedFiles int    `json:"accessed_files"`
	AccessCount   uint64 `json:"access_count"`
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

		// Validate APK packages if present
		if len(container.APKPackages) > 0 {
			fmt.Printf("  ✓ APK Packages: %d\n", len(container.APKPackages))
			if err := validateAPKPackages(container); err != nil {
				return fmt.Errorf("container %s APK validation: %w", container.Name, err)
			}

			// Show summary stats
			totalAPKFiles := 0
			accessedAPKFiles := 0
			packagesWithAccess := 0
			for _, pkg := range container.APKPackages {
				totalAPKFiles += pkg.TotalFiles
				accessedAPKFiles += pkg.AccessedFiles
				if pkg.AccessCount > 0 {
					packagesWithAccess++
				}
			}
			fmt.Printf("  APK Summary:\n")
			fmt.Printf("    Total APK files: %d\n", totalAPKFiles)
			fmt.Printf("    Accessed APK files: %d\n", accessedAPKFiles)
			fmt.Printf("    Packages with access: %d\n", packagesWithAccess)

			// Show top 3 packages by access count
			topPackages := getTopPackages(container.APKPackages, 3)
			if len(topPackages) > 0 {
				fmt.Printf("  Top packages:\n")
				for _, pkg := range topPackages {
					fmt.Printf("    - %s (%s): %d files accessed, %d total accesses\n",
						pkg.Name, pkg.Version, pkg.AccessedFiles, pkg.AccessCount)
				}
			}
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

	// Calculate APK aggregate statistics
	totalAPKPackages := 0
	totalAPKPackagesAccessed := 0
	totalAPKPackagesUnused := 0
	containersWithAPK := 0

	for _, container := range report.Containers {
		if len(container.APKPackages) > 0 {
			containersWithAPK++
			totalAPKPackages += len(container.APKPackages)
			for _, pkg := range container.APKPackages {
				if pkg.AccessCount > 0 {
					totalAPKPackagesAccessed++
				} else {
					totalAPKPackagesUnused++
				}
			}
		}
	}

	if containersWithAPK > 0 {
		fmt.Println("\n=== APK Package Statistics ===")
		fmt.Printf("Containers with APK databases: %d\n", containersWithAPK)
		fmt.Printf("Total APK packages: %d\n", totalAPKPackages)
		fmt.Printf("APK packages accessed: %d\n", totalAPKPackagesAccessed)
		fmt.Printf("APK packages NOT accessed: %d\n", totalAPKPackagesUnused)
		if totalAPKPackages > 0 {
			utilizationRate := float64(totalAPKPackagesAccessed) / float64(totalAPKPackages) * 100
			fmt.Printf("APK utilization rate: %.1f%%\n", utilizationRate)
		}
	} else {
		fmt.Println("\n=== APK Package Statistics ===")
		fmt.Println("⚠️  No APK databases detected in any container")
		fmt.Println("    This is expected in Kubernetes/containerd environments")
		fmt.Println("    APK detection requires /proc/{pid}/root filesystem access")
	}

	return nil
}

func validateAPKPackages(container ContainerReport) error {
	// Validate each APK package
	for _, pkg := range container.APKPackages {
		if pkg.Name == "" {
			return fmt.Errorf("APK package has empty name")
		}
		if pkg.Version == "" {
			return fmt.Errorf("APK package %s has empty version", pkg.Name)
		}
		if pkg.TotalFiles < 0 {
			return fmt.Errorf("APK package %s has negative total_files", pkg.Name)
		}
		if pkg.AccessedFiles < 0 {
			return fmt.Errorf("APK package %s has negative accessed_files", pkg.Name)
		}
		if pkg.AccessedFiles > pkg.TotalFiles {
			return fmt.Errorf("APK package %s has accessed_files (%d) > total_files (%d)",
				pkg.Name, pkg.AccessedFiles, pkg.TotalFiles)
		}
		// AccessCount can be 0 (for unused packages) or greater
		if pkg.AccessCount < 0 {
			return fmt.Errorf("APK package %s has negative access_count", pkg.Name)
		}
		// If AccessedFiles > 0, AccessCount should also be > 0
		if pkg.AccessedFiles > 0 && pkg.AccessCount == 0 {
			return fmt.Errorf("APK package %s has accessed files but zero access count", pkg.Name)
		}
	}

	// Check for duplicate package names
	seen := make(map[string]bool)
	for _, pkg := range container.APKPackages {
		if seen[pkg.Name] {
			return fmt.Errorf("duplicate APK package: %s", pkg.Name)
		}
		seen[pkg.Name] = true
	}

	return nil
}

func getTopPackages(packages []APKPackageReport, n int) []APKPackageReport {
	// Sort by access count (descending) and return top n
	sorted := make([]APKPackageReport, len(packages))
	copy(sorted, packages)

	// Simple bubble sort for small n
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].AccessCount > sorted[i].AccessCount {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	if len(sorted) > n {
		sorted = sorted[:n]
	}

	// Filter out packages with zero access
	result := []APKPackageReport{}
	for _, pkg := range sorted {
		if pkg.AccessCount > 0 {
			result = append(result, pkg)
		}
	}

	return result
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
