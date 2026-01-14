package config

import (
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestConfig_Validate(t *testing.T) {
	// Create a temporary directory for testing report path validation
	tmpDir := t.TempDir()

	for _, tt := range []struct {
		desc    string
		cfg     *Config
		wantErr bool
	}{
		{
			desc: "valid config with single cgroup",
			cfg: &Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
				MaxUniqueFiles: 1000,
			},
			wantErr: false,
		},
		{
			desc: "valid config with multiple cgroups",
			cfg: &Config{
				CgroupPaths:    []string{"/sys/fs/cgroup/test1", "/sys/fs/cgroup/test2"},
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
				MaxUniqueFiles: 1000,
			},
			wantErr: false,
		},
		{
			desc: "backwards compatibility - single cgroup migrated to CgroupPaths",
			cfg: &Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
			},
			wantErr: false,
		},
		{
			desc: "missing cgroup path",
			cfg: &Config{
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
			},
			wantErr: true,
		},
		{
			desc: "missing report path",
			cfg: &Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
			},
			wantErr: true,
		},
		{
			desc: "zero report interval",
			cfg: &Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 0,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
			},
			wantErr: true,
		},
		{
			desc: "negative report interval",
			cfg: &Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: -1 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
			},
			wantErr: true,
		},
		{
			desc: "report interval too short",
			cfg: &Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 500 * time.Millisecond,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
			},
			wantErr: true,
		},
		{
			desc: "negative max unique files",
			cfg: &Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
				MaxUniqueFiles: -1,
			},
			wantErr: true,
		},
		{
			desc: "nonexistent report directory",
			cfg: &Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     "/nonexistent/directory/report.json",
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
			},
			wantErr: true,
		},
		{
			desc: "invalid metrics address",
			cfg: &Config{
				CgroupPath:     "/sys/fs/cgroup/test",
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				MetricsAddr:    "invalid",
				LogLevel:       slog.LevelInfo,
			},
			wantErr: true,
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			// If validation succeeded and we had a single cgroup path, verify it was migrated
			if err == nil && tt.cfg.CgroupPath != "" && len(tt.cfg.CgroupPaths) == 0 {
				t.Error("Expected CgroupPath to be migrated to CgroupPaths, but it wasn't")
			}
		})
	}
}

func TestParseExcludePaths(t *testing.T) {
	for _, tt := range []struct {
		desc  string
		input string
		want  []string
	}{
		{
			desc:  "empty string",
			input: "",
			want:  nil,
		},
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
			input: " /proc/ , /sys/ , /dev/ ",
			want:  []string{"/proc/", "/sys/", "/dev/"},
		},
		{
			desc:  "trailing comma",
			input: "/proc/,/sys/,",
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

func TestParseCgroupPaths(t *testing.T) {
	for _, tt := range []struct {
		desc  string
		input string
		want  []string
	}{
		{
			desc:  "empty string",
			input: "",
			want:  nil,
		},
		{
			desc:  "single path",
			input: "/sys/fs/cgroup/test",
			want:  []string{"/sys/fs/cgroup/test"},
		},
		{
			desc:  "multiple paths",
			input: "/sys/fs/cgroup/test1,/sys/fs/cgroup/test2,/sys/fs/cgroup/test3",
			want:  []string{"/sys/fs/cgroup/test1", "/sys/fs/cgroup/test2", "/sys/fs/cgroup/test3"},
		},
		{
			desc:  "paths with spaces",
			input: " /sys/fs/cgroup/test1 , /sys/fs/cgroup/test2 ",
			want:  []string{"/sys/fs/cgroup/test1", "/sys/fs/cgroup/test2"},
		},
		{
			desc:  "trailing comma",
			input: "/sys/fs/cgroup/test1,/sys/fs/cgroup/test2,",
			want:  []string{"/sys/fs/cgroup/test1", "/sys/fs/cgroup/test2"},
		},
		{
			desc:  "kubernetes-style paths",
			input: "/kubepods/burstable/pod123/container1,/kubepods/burstable/pod123/container2",
			want:  []string{"/kubepods/burstable/pod123/container1", "/kubepods/burstable/pod123/container2"},
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			got := ParseCgroupPaths(tt.input)
			if len(got) != len(tt.want) {
				t.Errorf("ParseCgroupPaths() length = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ParseCgroupPaths()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExcludePathsString(t *testing.T) {
	cfg := &Config{
		ExcludePaths: []string{"/proc/", "/sys/", "/dev/"},
	}
	want := "/proc/,/sys/,/dev/"
	got := cfg.ExcludePathsString()
	if got != want {
		t.Errorf("ExcludePathsString() = %q, want %q", got, want)
	}
}

func TestConfig_ValidateBackwardsCompatibility(t *testing.T) {
	tmpDir := t.TempDir()

	// Test that old-style single cgroup path is migrated to new CgroupPaths slice
	cfg := &Config{
		CgroupPath:     "/sys/fs/cgroup/test",
		ReportPath:     filepath.Join(tmpDir, "report.json"),
		ReportInterval: 30 * time.Second,
		LogLevel:       slog.LevelInfo,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}

	if len(cfg.CgroupPaths) != 1 {
		t.Errorf("Expected CgroupPaths to have 1 entry, got %d", len(cfg.CgroupPaths))
	}

	if cfg.CgroupPaths[0] != "/sys/fs/cgroup/test" {
		t.Errorf("Expected CgroupPaths[0] = %q, got %q", "/sys/fs/cgroup/test", cfg.CgroupPaths[0])
	}
}

func TestConfig_ValidateBothCgroupFieldsSpecified(t *testing.T) {
	tmpDir := t.TempDir()

	// If both are specified, CgroupPaths should take precedence
	cfg := &Config{
		CgroupPath:     "/sys/fs/cgroup/old",
		CgroupPaths:    []string{"/sys/fs/cgroup/new1", "/sys/fs/cgroup/new2"},
		ReportPath:     filepath.Join(tmpDir, "report.json"),
		ReportInterval: 30 * time.Second,
		LogLevel:       slog.LevelInfo,
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}

	// CgroupPaths should remain as-is
	if len(cfg.CgroupPaths) != 2 {
		t.Errorf("Expected CgroupPaths to have 2 entries, got %d", len(cfg.CgroupPaths))
	}
}
