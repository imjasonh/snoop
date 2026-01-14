package processor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NormalizePath normalizes a file path by:
// - Resolving . and .. components
// - Converting relative paths to absolute using the provided working directory
// - Preserving symlinks (not following them)
//
// The pid parameter is used to look up the process's working directory
// via /proc/<pid>/cwd when the path is relative and cwd is empty.
func NormalizePath(path string, pid uint32, cwd string) string {
	if path == "" {
		return ""
	}

	// Handle absolute paths: just clean them
	if filepath.IsAbs(path) {
		return cleanPath(path)
	}

	// For relative paths, we need a working directory
	workDir := cwd
	if workDir == "" && pid > 0 {
		// Try to read the process's cwd from /proc
		workDir = getProcessCwd(pid)
	}
	if workDir == "" {
		// Fallback: prefix with / and clean the result
		// This is a best-effort when we can't determine the cwd
		return cleanPath("/" + path)
	}

	// Join with working directory and clean
	return cleanPath(filepath.Join(workDir, path))
}

// cleanPath removes . and .. components without following symlinks.
// Unlike filepath.Clean, this preserves trailing slashes and handles
// edge cases more carefully for our use case.
func cleanPath(path string) string {
	if path == "" {
		return ""
	}

	// filepath.Clean handles ., .., and multiple slashes
	cleaned := filepath.Clean(path)

	// Ensure absolute paths start with /
	if !strings.HasPrefix(cleaned, "/") && strings.HasPrefix(path, "/") {
		cleaned = "/" + cleaned
	}

	// Handle .. past root: /../foo should become /foo
	// filepath.Clean preserves this, but we need to strip leading /..
	// This loop strips all leading /../ sequences
	for strings.HasPrefix(cleaned, "/../") {
		cleaned = "/" + cleaned[4:] // Remove "/.." but keep the leading "/"
	}

	// Special case: if path is exactly "/..", it should become "/"
	if cleaned == "/.." {
		cleaned = "/"
	}

	return cleaned
}

// getProcessCwd reads the working directory of a process from /proc.
// Returns empty string if the process doesn't exist or cwd can't be read.
func getProcessCwd(pid uint32) string {
	// Read the symlink target of /proc/<pid>/cwd
	cwdPath := fmt.Sprintf("/proc/%d/cwd", pid)
	cwd, err := os.Readlink(cwdPath)
	if err != nil {
		return ""
	}
	return cwd
}

// IsExcluded checks if a path should be excluded based on the provided prefixes.
// Paths starting with any prefix in the exclusion list are excluded.
func IsExcluded(path string, excludePrefixes []string) bool {
	for _, prefix := range excludePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// DefaultExclusions returns the default path prefixes to exclude from tracing.
// These are system paths that are not relevant for image slimming.
func DefaultExclusions() []string {
	return []string{
		"/proc/",
		"/sys/",
		"/dev/",
	}
}
