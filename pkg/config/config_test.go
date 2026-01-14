package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConfig_Validate(t *testing.T) {
	// Create a temp directory for testing report path validation
	tempDir := t.TempDir()

	for _, tt := range []struct {
		desc    string
		config  Config
		wantErr string
	}{
		{
			desc: "valid config with all required fields",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/system.slice/docker-abc123.scope",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
				MetricsAddr:    ":9090",
				MaxUniqueFiles: 10000,
			},
			wantErr: "",
		},
		{
			desc: "valid config with minimal fields",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 1 * time.Second,
				LogLevel:       slog.LevelInfo,
			},
			wantErr: "",
		},
		{
			desc: "valid config with unbounded files",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
				MaxUniqueFiles: 0, // unbounded
			},
			wantErr: "",
		},
		{
			desc: "missing cgroup path",
			config: Config{
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
			},
			wantErr: "cgroup path is required",
		},
		{
			desc: "missing report path",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
			},
			wantErr: "report path is required",
		},
		{
			desc: "zero report interval",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 0,
				LogLevel:       slog.LevelInfo,
			},
			wantErr: "report interval must be positive",
		},
		{
			desc: "negative report interval",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: -5 * time.Second,
				LogLevel:       slog.LevelInfo,
			},
			wantErr: "report interval must be positive",
		},
		{
			desc: "report interval too short",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 500 * time.Millisecond,
				LogLevel:       slog.LevelInfo,
			},
			wantErr: "report interval must be at least 1 second",
		},
		{
			desc: "invalid log level",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.Level(999), // Invalid level
			},
			wantErr: "invalid log level",
		},
		{
			desc: "valid log level - debug",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelDebug,
			},
			wantErr: "",
		},
		{
			desc: "valid log level - warn",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelWarn,
			},
			wantErr: "",
		},
		{
			desc: "valid log level - error",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelError,
			},
			wantErr: "",
		},
		{
			desc: "valid log level - case insensitive",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
			},
			wantErr: "",
		},
		{
			desc: "negative max unique files",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
				MaxUniqueFiles: -1,
			},
			wantErr: "max unique files cannot be negative",
		},
		{
			desc: "report directory does not exist",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     "/nonexistent/directory/report.json",
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
			},
			wantErr: "report directory does not exist",
		},
		{
			desc: "invalid metrics address - no colon",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
				MetricsAddr:    "9090",
			},
			wantErr: "invalid metrics address format",
		},
		{
			desc: "valid metrics address - port only",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
				MetricsAddr:    ":9090",
			},
			wantErr: "",
		},
		{
			desc: "valid metrics address - host and port",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
				MetricsAddr:    "localhost:9090",
			},
			wantErr: "",
		},
		{
			desc: "empty metrics address is valid",
			config: Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tempDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
				MetricsAddr:    "",
			},
			wantErr: "",
		},
		{
			desc: "multiple validation errors",
			config: Config{
				CgroupPath:     "", // missing
				ReportPath:     "", // missing
				ReportInterval: 0,  // invalid
				LogLevel:       slog.Level(999), // Invalid level
				MaxUniqueFiles: -1,
			},
			wantErr: "configuration validation failed",
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("Validate() error = %v, want error containing %q", err, tt.wantErr)
				}
			}
		})
	}
}

func TestConfig_Validate_ReportPathDirectory(t *testing.T) {
	// Test case where parent of report path is a file, not a directory
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "file")
	if err := os.WriteFile(tempFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	config := Config{
		CgroupPath:     "/sys/fs/cgroup/test",
		ReportPath:     filepath.Join(tempFile, "report.json"), // parent is a file
		ReportInterval: 30 * time.Second,
		LogLevel:       slog.LevelInfo,
	}

	err := config.Validate()
	if err == nil {
		t.Error("Validate() expected error for report path parent being a file, got nil")
	} else if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("Validate() error = %v, want error containing 'not a directory'", err)
	}
}

func TestParseExcludePaths(t *testing.T) {
	for _, tt := range []struct {
		desc  string
		input string
		want  []string
	}{
		{
			desc:  "single path",
			input: "/proc/",
			want:  []string{"/proc/"},
		},
		{
			desc:  "multiple paths",
			input: "/proc/,/sys/,/dev/",
			want:  []string{"/proc/", "/sys/", "/dev/"},
		},
		{
			desc:  "paths with spaces",
			input: "/proc/ , /sys/ , /dev/",
			want:  []string{"/proc/", "/sys/", "/dev/"},
		},
		{
			desc:  "empty string",
			input: "",
			want:  nil,
		},
		{
			desc:  "only commas",
			input: ",,,",
			want:  []string{},
		},
		{
			desc:  "mixed empty and non-empty",
			input: "/proc/,,/sys/,",
			want:  []string{"/proc/", "/sys/"},
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			got := ParseExcludePaths(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("ParseExcludePaths() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseExcludePaths()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestConfig_ExcludePathsString(t *testing.T) {
	for _, tt := range []struct {
		desc  string
		paths []string
		want  string
	}{
		{
			desc:  "single path",
			paths: []string{"/proc/"},
			want:  "/proc/",
		},
		{
			desc:  "multiple paths",
			paths: []string{"/proc/", "/sys/", "/dev/"},
			want:  "/proc/,/sys/,/dev/",
		},
		{
			desc:  "empty slice",
			paths: []string{},
			want:  "",
		},
		{
			desc:  "nil slice",
			paths: nil,
			want:  "",
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			c := Config{ExcludePaths: tt.paths}
			got := c.ExcludePathsString()
			if got != tt.want {
				t.Errorf("ExcludePathsString() = %q, want %q", got, tt.want)
			}
		})
	}
}
