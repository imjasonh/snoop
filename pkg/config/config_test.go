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
			desc: "valid config with cgroup",
			cfg: &Config{
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
				MaxUniqueFiles: 1000,
			},
			wantErr: false,
		},
		{
			desc: "missing cgroup path is valid (auto-discovery)",
			cfg: &Config{
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
			},
			wantErr: false,
		},
		{
			desc: "missing report path",
			cfg: &Config{
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				LogLevel:       slog.LevelInfo,
			},
			wantErr: true,
		},
		{
			desc: "zero report interval",
			cfg: &Config{
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
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				ExcludePaths:   []string{"/proc/", "/sys/"},
				MetricsAddr:    "invalid",
				LogLevel:       slog.LevelInfo,
			},
			wantErr: true,
		},
		{
			desc: "valid metrics address - port only",
			cfg: &Config{
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
				MetricsAddr:    ":9090",
			},
			wantErr: false,
		},
		{
			desc: "valid metrics address - host and port",
			cfg: &Config{
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
				MetricsAddr:    "localhost:9090",
			},
			wantErr: false,
		},
		{
			desc: "empty metrics address is valid",
			cfg: &Config{
				ReportPath:     filepath.Join(tmpDir, "report.json"),
				ReportInterval: 30 * time.Second,
				LogLevel:       slog.LevelInfo,
				MetricsAddr:    "",
			},
			wantErr: false,
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
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
