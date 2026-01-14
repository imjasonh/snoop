package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Config holds the configuration for snoop.
type Config struct {
	// Target selection
	CgroupPath string

	// Output configuration
	ReportPath     string
	ReportInterval time.Duration

	// Filtering
	ExcludePaths []string

	// Metadata
	ImageRef    string
	ContainerID string
	PodName     string
	Namespace   string

	// Observability
	MetricsAddr string
	LogLevel    slog.Level

	// Resource limits
	MaxUniqueFiles int
}

// Validate checks that the configuration is valid and returns an error if not.
func (c *Config) Validate() error {
	var errs []string

	// Required fields
	if c.CgroupPath == "" {
		errs = append(errs, "cgroup path is required")
	}

	if c.ReportPath == "" {
		errs = append(errs, "report path is required")
	}

	// Validate report interval
	if c.ReportInterval <= 0 {
		errs = append(errs, "report interval must be positive")
	}
	if c.ReportInterval < time.Second {
		errs = append(errs, "report interval must be at least 1 second")
	}

	// Validate log level
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[strings.ToLower(c.LogLevel.String())] {
		errs = append(errs, fmt.Sprintf("invalid log level %q (must be debug, info, warn, or error)", c.LogLevel))
	}

	// Validate max unique files
	if c.MaxUniqueFiles < 0 {
		errs = append(errs, "max unique files cannot be negative")
	}

	// Validate report path is writable (check directory exists and is writable)
	if c.ReportPath != "" {
		dir := c.ReportPath
		// Get directory path
		if lastSlash := strings.LastIndex(c.ReportPath, "/"); lastSlash >= 0 {
			dir = c.ReportPath[:lastSlash]
			if dir == "" {
				dir = "/"
			}
		} else {
			dir = "."
		}

		// Check if directory exists
		info, err := os.Stat(dir)
		if err != nil {
			if os.IsNotExist(err) {
				errs = append(errs, fmt.Sprintf("report directory does not exist: %s", dir))
			} else {
				errs = append(errs, fmt.Sprintf("cannot stat report directory: %v", err))
			}
		} else if !info.IsDir() {
			errs = append(errs, fmt.Sprintf("report path parent is not a directory: %s", dir))
		}
	}

	// Validate metrics address format if provided
	if c.MetricsAddr != "" {
		// Basic validation: should have format :port or host:port
		if !strings.Contains(c.MetricsAddr, ":") {
			errs = append(errs, fmt.Sprintf("invalid metrics address format %q (expected :port or host:port)", c.MetricsAddr))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// ExcludePathsString returns the exclude paths as a comma-separated string.
func (c *Config) ExcludePathsString() string {
	return strings.Join(c.ExcludePaths, ",")
}

// ParseExcludePaths parses a comma-separated string of exclude paths.
func ParseExcludePaths(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
