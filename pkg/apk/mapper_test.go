package apk

import (
	"sync"
	"testing"
)

func TestNewMapper(t *testing.T) {
	db := &Database{
		Packages: map[string]*Package{
			"test-pkg": {
				Name:    "test-pkg",
				Version: "1.0.0",
				Files:   []string{"/usr/bin/test"},
			},
		},
		FileToPackage: map[string]string{
			"/usr/bin/test": "test-pkg",
		},
	}

	mapper := NewMapper(db)

	if mapper == nil {
		t.Fatal("NewMapper returned nil")
	}
	if mapper.db != db {
		t.Error("mapper database not set correctly")
	}
	if mapper.accesses == nil {
		t.Error("mapper accesses map not initialized")
	}
}

func TestRecordAccess(t *testing.T) {
	for _, tt := range []struct {
		desc            string
		db              *Database
		accesses        []string
		wantPkgAccesses map[string]uint64
		wantFileCount   map[string]int
	}{
		{
			desc: "single access to known file",
			db: &Database{
				Packages: map[string]*Package{
					"pkg1": {
						Name:    "pkg1",
						Version: "1.0.0",
						Files:   []string{"/file1"},
					},
				},
				FileToPackage: map[string]string{
					"/file1": "pkg1",
				},
			},
			accesses: []string{"/file1"},
			wantPkgAccesses: map[string]uint64{
				"pkg1": 1,
			},
			wantFileCount: map[string]int{
				"pkg1": 1,
			},
		},
		{
			desc: "multiple accesses to same file",
			db: &Database{
				Packages: map[string]*Package{
					"pkg1": {
						Name:    "pkg1",
						Version: "1.0.0",
						Files:   []string{"/file1"},
					},
				},
				FileToPackage: map[string]string{
					"/file1": "pkg1",
				},
			},
			accesses: []string{"/file1", "/file1", "/file1"},
			wantPkgAccesses: map[string]uint64{
				"pkg1": 3,
			},
			wantFileCount: map[string]int{
				"pkg1": 1, // Still only 1 unique file
			},
		},
		{
			desc: "accesses to multiple files in same package",
			db: &Database{
				Packages: map[string]*Package{
					"pkg1": {
						Name:    "pkg1",
						Version: "1.0.0",
						Files:   []string{"/file1", "/file2", "/file3"},
					},
				},
				FileToPackage: map[string]string{
					"/file1": "pkg1",
					"/file2": "pkg1",
					"/file3": "pkg1",
				},
			},
			accesses: []string{"/file1", "/file2", "/file1", "/file3"},
			wantPkgAccesses: map[string]uint64{
				"pkg1": 4,
			},
			wantFileCount: map[string]int{
				"pkg1": 3, // All 3 files accessed
			},
		},
		{
			desc: "accesses to multiple packages",
			db: &Database{
				Packages: map[string]*Package{
					"pkg1": {
						Name:    "pkg1",
						Version: "1.0.0",
						Files:   []string{"/file1"},
					},
					"pkg2": {
						Name:    "pkg2",
						Version: "2.0.0",
						Files:   []string{"/file2"},
					},
				},
				FileToPackage: map[string]string{
					"/file1": "pkg1",
					"/file2": "pkg2",
				},
			},
			accesses: []string{"/file1", "/file2", "/file1"},
			wantPkgAccesses: map[string]uint64{
				"pkg1": 2,
				"pkg2": 1,
			},
			wantFileCount: map[string]int{
				"pkg1": 1,
				"pkg2": 1,
			},
		},
		{
			desc: "access to unknown file (not in database)",
			db: &Database{
				Packages: map[string]*Package{
					"pkg1": {
						Name:    "pkg1",
						Version: "1.0.0",
						Files:   []string{"/file1"},
					},
				},
				FileToPackage: map[string]string{
					"/file1": "pkg1",
				},
			},
			accesses: []string{"/unknown/file", "/file1"},
			wantPkgAccesses: map[string]uint64{
				"pkg1": 1,
			},
			wantFileCount: map[string]int{
				"pkg1": 1,
			},
		},
		{
			desc: "no accesses",
			db: &Database{
				Packages: map[string]*Package{
					"pkg1": {
						Name:    "pkg1",
						Version: "1.0.0",
						Files:   []string{"/file1"},
					},
				},
				FileToPackage: map[string]string{
					"/file1": "pkg1",
				},
			},
			accesses:        []string{},
			wantPkgAccesses: map[string]uint64{},
			wantFileCount:   map[string]int{},
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			mapper := NewMapper(tt.db)

			// Record all accesses
			for _, path := range tt.accesses {
				mapper.RecordAccess(path)
			}

			// Verify access counts
			mapper.mu.RLock()
			defer mapper.mu.RUnlock()

			for pkgName, wantCount := range tt.wantPkgAccesses {
				access, exists := mapper.accesses[pkgName]
				if !exists {
					t.Errorf("package %q not found in accesses map", pkgName)
					continue
				}
				if access.totalCount != wantCount {
					t.Errorf("package %q: got %d accesses, want %d", pkgName, access.totalCount, wantCount)
				}
			}

			// Verify file counts
			for pkgName, wantFileCount := range tt.wantFileCount {
				access, exists := mapper.accesses[pkgName]
				if !exists {
					t.Errorf("package %q not found in accesses map", pkgName)
					continue
				}
				if len(access.accessedFiles) != wantFileCount {
					t.Errorf("package %q: got %d accessed files, want %d", pkgName, len(access.accessedFiles), wantFileCount)
				}
			}

			// Verify no extra packages in accesses
			if len(mapper.accesses) != len(tt.wantPkgAccesses) {
				t.Errorf("got %d packages in accesses, want %d", len(mapper.accesses), len(tt.wantPkgAccesses))
			}
		})
	}
}

func TestStats(t *testing.T) {
	for _, tt := range []struct {
		desc     string
		db       *Database
		accesses []string
		want     []PackageStats
	}{
		{
			desc: "package with accesses",
			db: &Database{
				Packages: map[string]*Package{
					"pkg1": {
						Name:    "pkg1",
						Version: "1.0.0",
						Files:   []string{"/file1", "/file2"},
					},
				},
				FileToPackage: map[string]string{
					"/file1": "pkg1",
					"/file2": "pkg1",
				},
			},
			accesses: []string{"/file1", "/file1", "/file2"},
			want: []PackageStats{
				{
					Name:          "pkg1",
					Version:       "1.0.0",
					TotalFiles:    2,
					AccessedFiles: 2,
					AccessCount:   3,
				},
			},
		},
		{
			desc: "package with zero accesses",
			db: &Database{
				Packages: map[string]*Package{
					"pkg1": {
						Name:    "pkg1",
						Version: "1.0.0",
						Files:   []string{"/file1", "/file2"},
					},
				},
				FileToPackage: map[string]string{
					"/file1": "pkg1",
					"/file2": "pkg1",
				},
			},
			accesses: []string{},
			want: []PackageStats{
				{
					Name:          "pkg1",
					Version:       "1.0.0",
					TotalFiles:    2,
					AccessedFiles: 0,
					AccessCount:   0,
				},
			},
		},
		{
			desc: "multiple packages with mixed accesses",
			db: &Database{
				Packages: map[string]*Package{
					"pkg-a": {
						Name:    "pkg-a",
						Version: "1.0.0",
						Files:   []string{"/a1", "/a2"},
					},
					"pkg-b": {
						Name:    "pkg-b",
						Version: "2.0.0",
						Files:   []string{"/b1"},
					},
					"pkg-c": {
						Name:    "pkg-c",
						Version: "3.0.0",
						Files:   []string{"/c1", "/c2", "/c3"},
					},
				},
				FileToPackage: map[string]string{
					"/a1": "pkg-a",
					"/a2": "pkg-a",
					"/b1": "pkg-b",
					"/c1": "pkg-c",
					"/c2": "pkg-c",
					"/c3": "pkg-c",
				},
			},
			accesses: []string{"/a1", "/c1", "/c2", "/c1"},
			want: []PackageStats{
				{
					Name:          "pkg-a",
					Version:       "1.0.0",
					TotalFiles:    2,
					AccessedFiles: 1,
					AccessCount:   1,
				},
				{
					Name:          "pkg-b",
					Version:       "2.0.0",
					TotalFiles:    1,
					AccessedFiles: 0,
					AccessCount:   0,
				},
				{
					Name:          "pkg-c",
					Version:       "3.0.0",
					TotalFiles:    3,
					AccessedFiles: 2,
					AccessCount:   3,
				},
			},
		},
		{
			desc: "package with no files",
			db: &Database{
				Packages: map[string]*Package{
					"virtual-pkg": {
						Name:    "virtual-pkg",
						Version: "1.0.0",
						Files:   []string{},
					},
				},
				FileToPackage: map[string]string{},
			},
			accesses: []string{},
			want: []PackageStats{
				{
					Name:          "virtual-pkg",
					Version:       "1.0.0",
					TotalFiles:    0,
					AccessedFiles: 0,
					AccessCount:   0,
				},
			},
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			mapper := NewMapper(tt.db)

			// Record all accesses
			for _, path := range tt.accesses {
				mapper.RecordAccess(path)
			}

			// Get stats
			got := mapper.Stats()

			// Verify length
			if len(got) != len(tt.want) {
				t.Fatalf("got %d stats, want %d", len(got), len(tt.want))
			}

			// Verify each stat (order is sorted by name)
			for i, wantStat := range tt.want {
				gotStat := got[i]

				if gotStat.Name != wantStat.Name {
					t.Errorf("stats[%d]: got name %q, want %q", i, gotStat.Name, wantStat.Name)
				}
				if gotStat.Version != wantStat.Version {
					t.Errorf("stats[%d]: got version %q, want %q", i, gotStat.Version, wantStat.Version)
				}
				if gotStat.TotalFiles != wantStat.TotalFiles {
					t.Errorf("stats[%d] (%s): got %d total files, want %d", i, gotStat.Name, gotStat.TotalFiles, wantStat.TotalFiles)
				}
				if gotStat.AccessedFiles != wantStat.AccessedFiles {
					t.Errorf("stats[%d] (%s): got %d accessed files, want %d", i, gotStat.Name, gotStat.AccessedFiles, wantStat.AccessedFiles)
				}
				if gotStat.AccessCount != wantStat.AccessCount {
					t.Errorf("stats[%d] (%s): got %d access count, want %d", i, gotStat.Name, gotStat.AccessCount, wantStat.AccessCount)
				}
			}
		})
	}
}

func TestStats_Sorted(t *testing.T) {
	// Verify that Stats returns results sorted by package name
	db := &Database{
		Packages: map[string]*Package{
			"zebra": {Name: "zebra", Version: "1.0.0", Files: []string{"/z"}},
			"alpha": {Name: "alpha", Version: "1.0.0", Files: []string{"/a"}},
			"beta":  {Name: "beta", Version: "1.0.0", Files: []string{"/b"}},
		},
		FileToPackage: map[string]string{
			"/z": "zebra",
			"/a": "alpha",
			"/b": "beta",
		},
	}

	mapper := NewMapper(db)
	stats := mapper.Stats()

	expectedOrder := []string{"alpha", "beta", "zebra"}
	if len(stats) != len(expectedOrder) {
		t.Fatalf("got %d stats, want %d", len(stats), len(expectedOrder))
	}

	for i, expectedName := range expectedOrder {
		if stats[i].Name != expectedName {
			t.Errorf("stats[%d]: got name %q, want %q", i, stats[i].Name, expectedName)
		}
	}
}

func TestRecordAccess_Concurrent(t *testing.T) {
	// Test thread safety with concurrent access recording
	db := &Database{
		Packages: map[string]*Package{
			"pkg1": {
				Name:    "pkg1",
				Version: "1.0.0",
				Files:   []string{"/file1", "/file2", "/file3"},
			},
			"pkg2": {
				Name:    "pkg2",
				Version: "2.0.0",
				Files:   []string{"/file4", "/file5"},
			},
		},
		FileToPackage: map[string]string{
			"/file1": "pkg1",
			"/file2": "pkg1",
			"/file3": "pkg1",
			"/file4": "pkg2",
			"/file5": "pkg2",
		},
	}

	mapper := NewMapper(db)

	// Number of goroutines and accesses per goroutine
	numGoroutines := 10
	accessesPerGoroutine := 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Launch concurrent goroutines
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			// Each goroutine alternates between files
			for j := 0; j < accessesPerGoroutine; j++ {
				if j%2 == 0 {
					mapper.RecordAccess("/file1")
				} else {
					mapper.RecordAccess("/file4")
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify results
	stats := mapper.Stats()

	if len(stats) != 2 {
		t.Fatalf("got %d packages, want 2", len(stats))
	}

	// Find pkg1 and pkg2 stats
	var pkg1Stats, pkg2Stats *PackageStats
	for i := range stats {
		switch stats[i].Name {
		case "pkg1":
			pkg1Stats = &stats[i]
		case "pkg2":
			pkg2Stats = &stats[i]
		}
	}

	if pkg1Stats == nil || pkg2Stats == nil {
		t.Fatal("could not find pkg1 or pkg2 in stats")
	}

	// Expected: 10 goroutines * 100 accesses / 2 (alternating) = 500 accesses per package
	expectedAccesses := uint64(numGoroutines * accessesPerGoroutine / 2)

	if pkg1Stats.AccessCount != expectedAccesses {
		t.Errorf("pkg1: got %d accesses, want %d", pkg1Stats.AccessCount, expectedAccesses)
	}
	if pkg2Stats.AccessCount != expectedAccesses {
		t.Errorf("pkg2: got %d accesses, want %d", pkg2Stats.AccessCount, expectedAccesses)
	}

	// Each package should have 1 accessed file
	if pkg1Stats.AccessedFiles != 1 {
		t.Errorf("pkg1: got %d accessed files, want 1", pkg1Stats.AccessedFiles)
	}
	if pkg2Stats.AccessedFiles != 1 {
		t.Errorf("pkg2: got %d accessed files, want 1", pkg2Stats.AccessedFiles)
	}
}

func TestStats_Concurrent(t *testing.T) {
	// Test that Stats() can be called concurrently with RecordAccess()
	db := &Database{
		Packages: map[string]*Package{
			"pkg1": {
				Name:    "pkg1",
				Version: "1.0.0",
				Files:   []string{"/file1"},
			},
		},
		FileToPackage: map[string]string{
			"/file1": "pkg1",
		},
	}

	mapper := NewMapper(db)

	var wg sync.WaitGroup

	// Start goroutines that record accesses
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				mapper.RecordAccess("/file1")
			}
		}()
	}

	// Start goroutines that read stats
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				stats := mapper.Stats()
				// Just verify we got some stats, don't check exact values
				// since we're racing with writers
				if len(stats) != 1 {
					t.Errorf("expected 1 package in stats, got %d", len(stats))
				}
			}
		}()
	}

	wg.Wait()

	// Final verification
	stats := mapper.Stats()
	if len(stats) != 1 {
		t.Fatalf("got %d packages, want 1", len(stats))
	}

	// Should have exactly 500 accesses (5 goroutines * 100 accesses)
	expectedAccesses := uint64(500)
	if stats[0].AccessCount != expectedAccesses {
		t.Errorf("got %d accesses, want %d", stats[0].AccessCount, expectedAccesses)
	}
}
