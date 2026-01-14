package processor

import (
	"os"
	"testing"
)

func TestNormalizePath(t *testing.T) {
	for _, tt := range []struct {
		desc string
		path string
		pid  uint32
		cwd  string
		want string
	}{{
		desc: "empty path",
		path: "",
		want: "",
	}, {
		desc: "absolute path unchanged",
		path: "/etc/passwd",
		want: "/etc/passwd",
	}, {
		desc: "absolute path with single dot",
		path: "/etc/./passwd",
		want: "/etc/passwd",
	}, {
		desc: "absolute path with double dots",
		path: "/etc/nginx/../passwd",
		want: "/etc/passwd",
	}, {
		desc: "absolute path with multiple dots",
		path: "/usr/local/./bin/../lib/./test",
		want: "/usr/local/lib/test",
	}, {
		desc: "absolute path with leading double dots",
		path: "/../etc/passwd",
		want: "/etc/passwd",
	}, {
		desc: "absolute path with multiple slashes",
		path: "/etc//nginx///conf.d////default.conf",
		want: "/etc/nginx/conf.d/default.conf",
	}, {
		desc: "relative path with cwd",
		path: "config/app.yaml",
		cwd:  "/home/user/myapp",
		want: "/home/user/myapp/config/app.yaml",
	}, {
		desc: "relative path with dots and cwd",
		path: "./config/../data/file.txt",
		cwd:  "/app",
		want: "/app/data/file.txt",
	}, {
		desc: "relative path with parent traversal",
		path: "../shared/lib.so",
		cwd:  "/app/bin",
		want: "/app/shared/lib.so",
	}, {
		desc: "relative path without cwd or pid falls back",
		path: "some/file.txt",
		want: "/some/file.txt",
	}, {
		desc: "root path",
		path: "/",
		want: "/",
	}, {
		desc: "dot only path with cwd",
		path: ".",
		cwd:  "/home/user",
		want: "/home/user",
	}, {
		desc: "double dot only path with cwd",
		path: "..",
		cwd:  "/home/user",
		want: "/home",
	}, {
		desc: "complex traversal",
		path: "/a/b/c/../../d/./e/../f",
		want: "/a/d/f",
	}, {
		desc: "double dots past root",
		path: "/a/../../b",
		want: "/b",
	}} {
		t.Run(tt.desc, func(t *testing.T) {
			got := NormalizePath(tt.path, tt.pid, tt.cwd)
			if got != tt.want {
				t.Errorf("NormalizePath(%q, %d, %q) = %q, want %q",
					tt.path, tt.pid, tt.cwd, got, tt.want)
			}
		})
	}
}

func TestNormalizePathWithPid(t *testing.T) {
	// This test uses the current process's cwd via /proc
	// Skip if /proc is not available
	if _, err := os.Stat("/proc/self/cwd"); err != nil {
		t.Skip("skipping: /proc not available")
	}

	currentCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}

	pid := uint32(os.Getpid())
	got := NormalizePath("relative/path", pid, "")

	want := currentCwd + "/relative/path"
	if got != want {
		t.Errorf("NormalizePath with pid lookup = %q, want %q", got, want)
	}
}

func TestCleanPath(t *testing.T) {
	for _, tt := range []struct {
		desc string
		path string
		want string
	}{{
		desc: "empty path",
		path: "",
		want: "",
	}, {
		desc: "simple path",
		path: "/usr/bin/test",
		want: "/usr/bin/test",
	}, {
		desc: "single dot",
		path: "/usr/./bin",
		want: "/usr/bin",
	}, {
		desc: "double dot",
		path: "/usr/local/../bin",
		want: "/usr/bin",
	}, {
		desc: "multiple slashes",
		path: "/usr///bin//test",
		want: "/usr/bin/test",
	}, {
		desc: "trailing dot",
		path: "/usr/bin/.",
		want: "/usr/bin",
	}, {
		desc: "only dots",
		path: "/./././",
		want: "/",
	}} {
		t.Run(tt.desc, func(t *testing.T) {
			got := cleanPath(tt.path)
			if got != tt.want {
				t.Errorf("cleanPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsExcluded(t *testing.T) {
	exclusions := []string{"/proc/", "/sys/", "/dev/"}

	for _, tt := range []struct {
		desc string
		path string
		want bool
	}{{
		desc: "proc path excluded",
		path: "/proc/self/status",
		want: true,
	}, {
		desc: "sys path excluded",
		path: "/sys/kernel/debug/tracing",
		want: true,
	}, {
		desc: "dev path excluded",
		path: "/dev/null",
		want: true,
	}, {
		desc: "etc path not excluded",
		path: "/etc/passwd",
		want: false,
	}, {
		desc: "usr path not excluded",
		path: "/usr/bin/bash",
		want: false,
	}, {
		desc: "empty path not excluded",
		path: "",
		want: false,
	}, {
		desc: "proc-like path not excluded",
		path: "/var/proc/data",
		want: false,
	}, {
		desc: "exact proc prefix",
		path: "/proc/",
		want: true,
	}} {
		t.Run(tt.desc, func(t *testing.T) {
			got := IsExcluded(tt.path, exclusions)
			if got != tt.want {
				t.Errorf("IsExcluded(%q, %v) = %v, want %v",
					tt.path, exclusions, got, tt.want)
			}
		})
	}
}

func TestIsExcludedEmptyList(t *testing.T) {
	// With no exclusions, nothing should be excluded
	if IsExcluded("/proc/self/status", nil) {
		t.Error("IsExcluded with nil exclusions should return false")
	}
	if IsExcluded("/proc/self/status", []string{}) {
		t.Error("IsExcluded with empty exclusions should return false")
	}
}

func TestDefaultExclusions(t *testing.T) {
	exclusions := DefaultExclusions()

	// Verify we have the expected defaults
	expected := map[string]bool{
		"/proc/": true,
		"/sys/":  true,
		"/dev/":  true,
	}

	if len(exclusions) != len(expected) {
		t.Errorf("DefaultExclusions() returned %d items, want %d",
			len(exclusions), len(expected))
	}

	for _, ex := range exclusions {
		if !expected[ex] {
			t.Errorf("unexpected exclusion: %q", ex)
		}
	}
}
