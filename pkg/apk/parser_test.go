package apk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDatabase(t *testing.T) {
	for _, tt := range []struct {
		desc    string
		content string
		want    *Database
		wantErr bool
	}{
		{
			desc: "valid database with single package",
			content: `P:alpine-baselayout
V:3.4.3-r2
F:etc
F:etc/fstab
F:usr/bin/hello

`,
			want: &Database{
				Packages: map[string]*Package{
					"alpine-baselayout": {
						Name:    "alpine-baselayout",
						Version: "3.4.3-r2",
						Files:   []string{"/etc", "/etc/fstab", "/usr/bin/hello"},
					},
				},
				FileToPackage: map[string]string{
					"/etc":           "alpine-baselayout",
					"/etc/fstab":     "alpine-baselayout",
					"/usr/bin/hello": "alpine-baselayout",
				},
			},
			wantErr: false,
		},
		{
			desc: "valid database with multiple packages",
			content: `P:alpine-baselayout
V:3.4.3-r2
F:etc
F:etc/fstab

P:busybox
V:1.36.1-r5
F:bin/sh
F:bin/busybox

P:ca-certificates
V:20230506-r0
F:etc/ssl/certs

`,
			want: &Database{
				Packages: map[string]*Package{
					"alpine-baselayout": {
						Name:    "alpine-baselayout",
						Version: "3.4.3-r2",
						Files:   []string{"/etc", "/etc/fstab"},
					},
					"busybox": {
						Name:    "busybox",
						Version: "1.36.1-r5",
						Files:   []string{"/bin/sh", "/bin/busybox"},
					},
					"ca-certificates": {
						Name:    "ca-certificates",
						Version: "20230506-r0",
						Files:   []string{"/etc/ssl/certs"},
					},
				},
				FileToPackage: map[string]string{
					"/etc":           "alpine-baselayout",
					"/etc/fstab":     "alpine-baselayout",
					"/bin/sh":        "busybox",
					"/bin/busybox":   "busybox",
					"/etc/ssl/certs": "ca-certificates",
				},
			},
			wantErr: false,
		},
		{
			desc: "package with no files",
			content: `P:virtual-package
V:1.0.0-r0

`,
			want: &Database{
				Packages: map[string]*Package{
					"virtual-package": {
						Name:    "virtual-package",
						Version: "1.0.0-r0",
						Files:   []string{},
					},
				},
				FileToPackage: map[string]string{},
			},
			wantErr: false,
		},
		{
			desc: "package with version but missing package name",
			content: `V:1.0.0-r0
F:some/file

P:valid-package
V:2.0.0-r0
F:other/file

`,
			want: &Database{
				Packages: map[string]*Package{
					"valid-package": {
						Name:    "valid-package",
						Version: "2.0.0-r0",
						Files:   []string{"/other/file"},
					},
				},
				FileToPackage: map[string]string{
					"/other/file": "valid-package",
				},
			},
			wantErr: false,
		},
		{
			desc: "duplicate file across packages (first wins)",
			content: `P:package-one
V:1.0.0-r0
F:shared/file

P:package-two
V:2.0.0-r0
F:shared/file

`,
			want: &Database{
				Packages: map[string]*Package{
					"package-one": {
						Name:    "package-one",
						Version: "1.0.0-r0",
						Files:   []string{"/shared/file"},
					},
					"package-two": {
						Name:    "package-two",
						Version: "2.0.0-r0",
						Files:   []string{"/shared/file"},
					},
				},
				FileToPackage: map[string]string{
					"/shared/file": "package-one",
				},
			},
			wantErr: false,
		},
		{
			desc: "files with absolute paths (should preserve)",
			content: `P:test-pkg
V:1.0.0-r0
F:/absolute/path
F:relative/path

`,
			want: &Database{
				Packages: map[string]*Package{
					"test-pkg": {
						Name:    "test-pkg",
						Version: "1.0.0-r0",
						Files:   []string{"/absolute/path", "/relative/path"},
					},
				},
				FileToPackage: map[string]string{
					"/absolute/path": "test-pkg",
					"/relative/path": "test-pkg",
				},
			},
			wantErr: false,
		},
		{
			desc: "malformed lines (should skip)",
			content: `P:test-pkg
V:1.0.0-r0
this line has no colon
X:unknown-key-is-ok
F:valid/file
invalid:key:too:many:colons

`,
			want: &Database{
				Packages: map[string]*Package{
					"test-pkg": {
						Name:    "test-pkg",
						Version: "1.0.0-r0",
						Files:   []string{"/valid/file"},
					},
				},
				FileToPackage: map[string]string{
					"/valid/file": "test-pkg",
				},
			},
			wantErr: false,
		},
		{
			desc: "extra metadata fields (should ignore)",
			content: `P:full-package
V:3.4.3-r2
A:x86_64
S:413696
I:413696
T:Alpine base dir structure and init scripts
U:https://git.alpinelinux.org/cgit/aports/tree/main/alpine-baselayout
L:GPL-2.0-only
o:alpine-baselayout
m:Natanael Copa <ncopa@alpinelinux.org>
t:1234567890
c:abcdef1234567890
F:etc
R:ca-certificates
a:0:0:755
Z:Q1abc123...
F:etc/fstab
a:0:0:644
Z:Q1def456...

`,
			want: &Database{
				Packages: map[string]*Package{
					"full-package": {
						Name:    "full-package",
						Version: "3.4.3-r2",
						Files:   []string{"/etc", "/etc/fstab"},
					},
				},
				FileToPackage: map[string]string{
					"/etc":       "full-package",
					"/etc/fstab": "full-package",
				},
			},
			wantErr: false,
		},
		{
			desc:    "empty database",
			content: "",
			want:    nil,
			wantErr: true,
		},
		{
			desc: "only blank lines",
			content: `


`,
			want:    nil,
			wantErr: true,
		},
		{
			desc: "no package names",
			content: `V:1.0.0-r0
F:some/file

`,
			want:    nil,
			wantErr: true,
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			// Create temp file with test content
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "installed")
			if err := os.WriteFile(dbPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write test database: %v", err)
			}

			// Parse the database
			got, err := ParseDatabase(dbPath)

			// Check error expectations
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseDatabase() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			// Compare packages
			if len(got.Packages) != len(tt.want.Packages) {
				t.Errorf("got %d packages, want %d", len(got.Packages), len(tt.want.Packages))
			}

			for name, wantPkg := range tt.want.Packages {
				gotPkg, exists := got.Packages[name]
				if !exists {
					t.Errorf("package %q not found in result", name)
					continue
				}

				if gotPkg.Name != wantPkg.Name {
					t.Errorf("package %q: got name %q, want %q", name, gotPkg.Name, wantPkg.Name)
				}

				if gotPkg.Version != wantPkg.Version {
					t.Errorf("package %q: got version %q, want %q", name, gotPkg.Version, wantPkg.Version)
				}

				if len(gotPkg.Files) != len(wantPkg.Files) {
					t.Errorf("package %q: got %d files, want %d", name, len(gotPkg.Files), len(wantPkg.Files))
					t.Errorf("  got files: %v", gotPkg.Files)
					t.Errorf("  want files: %v", wantPkg.Files)
					continue
				}

				// Check files in order
				for i, wantFile := range wantPkg.Files {
					if gotPkg.Files[i] != wantFile {
						t.Errorf("package %q file[%d]: got %q, want %q", name, i, gotPkg.Files[i], wantFile)
					}
				}
			}

			// Compare file-to-package mapping
			if len(got.FileToPackage) != len(tt.want.FileToPackage) {
				t.Errorf("got %d file mappings, want %d", len(got.FileToPackage), len(tt.want.FileToPackage))
			}

			for file, wantPkg := range tt.want.FileToPackage {
				gotPkg, exists := got.FileToPackage[file]
				if !exists {
					t.Errorf("file %q not found in FileToPackage map", file)
					continue
				}
				if gotPkg != wantPkg {
					t.Errorf("file %q: got package %q, want %q", file, gotPkg, wantPkg)
				}
			}
		})
	}
}

func TestParseDatabase_FileNotFound(t *testing.T) {
	_, err := ParseDatabase("/nonexistent/path/to/database")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestParseDatabase_RealAPKDatabase(t *testing.T) {
	// This test uses a realistic APK database sample from Alpine Linux
	content := `C:Q1JgjFttr9IAuxIapGIRSyQNPVaUc=
P:alpine-baselayout
V:3.4.3-r2
A:x86_64
S:413696
I:413696
T:Alpine base dir structure and init scripts
U:https://git.alpinelinux.org/cgit/aports/tree/main/alpine-baselayout
L:GPL-2.0-only
o:alpine-baselayout
m:Natanael Copa <ncopa@alpinelinux.org>
t:1699459200
c:3b8f43d3e2f4f7b5c8e3d2f1e0d9c8b7a6f5e4d3
F:etc
R:alpine-baselayout-data
a:0:0:755
Z:Q1JgjFttr9IAuxIapGIRSyQNPVaUc=
F:etc/fstab
a:0:0:644
Z:Q1JgjFttr9IAuxIapGIRSyQNPVaUc=
F:etc/group
a:0:0:644
Z:Q1JgjFttr9IAuxIapGIRSyQNPVaUc=

C:Q1abcdefghijklmnopqrstuvwxyz123=
P:busybox
V:1.36.1-r5
A:x86_64
S:507904
I:962560
T:Size optimized toolbox of many common UNIX utilities
U:https://busybox.net/
L:GPL-2.0-only
o:busybox
m:Sören Tempel <soeren+alpine@soeren-tempel.net>
t:1699459200
c:1234567890abcdef1234567890abcdef12345678
F:bin
a:0:0:755
Z:Q1xyz=
F:bin/busybox
a:0:0:755
Z:Q1abc=
F:bin/sh
a:0:0:777
Z:Q1def=

`

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "installed")
	if err := os.WriteFile(dbPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test database: %v", err)
	}

	db, err := ParseDatabase(dbPath)
	if err != nil {
		t.Fatalf("ParseDatabase() failed: %v", err)
	}

	// Check we got both packages
	if len(db.Packages) != 2 {
		t.Errorf("got %d packages, want 2", len(db.Packages))
	}

	// Check alpine-baselayout
	baselayout, exists := db.Packages["alpine-baselayout"]
	if !exists {
		t.Fatal("alpine-baselayout not found")
	}
	if baselayout.Version != "3.4.3-r2" {
		t.Errorf("alpine-baselayout version: got %q, want %q", baselayout.Version, "3.4.3-r2")
	}
	expectedBaseFiles := []string{"/etc", "/etc/fstab", "/etc/group"}
	if len(baselayout.Files) != len(expectedBaseFiles) {
		t.Errorf("alpine-baselayout files: got %d, want %d", len(baselayout.Files), len(expectedBaseFiles))
	}

	// Check busybox
	busybox, exists := db.Packages["busybox"]
	if !exists {
		t.Fatal("busybox not found")
	}
	if busybox.Version != "1.36.1-r5" {
		t.Errorf("busybox version: got %q, want %q", busybox.Version, "1.36.1-r5")
	}
	expectedBusyboxFiles := []string{"/bin", "/bin/busybox", "/bin/sh"}
	if len(busybox.Files) != len(expectedBusyboxFiles) {
		t.Errorf("busybox files: got %d, want %d", len(busybox.Files), len(expectedBusyboxFiles))
	}

	// Check file mappings
	if db.FileToPackage["/etc/fstab"] != "alpine-baselayout" {
		t.Errorf("/etc/fstab mapped to %q, want alpine-baselayout", db.FileToPackage["/etc/fstab"])
	}
	if db.FileToPackage["/bin/sh"] != "busybox" {
		t.Errorf("/bin/sh mapped to %q, want busybox", db.FileToPackage["/bin/sh"])
	}
}
