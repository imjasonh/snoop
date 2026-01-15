package apk

import (
	"sort"
	"sync"
)

// PackageStats holds access statistics for a single package.
type PackageStats struct {
	Name          string // Package name
	Version       string // Package version
	TotalFiles    int    // Number of files in package
	AccessedFiles int    // Number of files accessed during window
	AccessCount   uint64 // Total number of accesses to files in this package
}

// Mapper tracks file access counts per package.
type Mapper struct {
	db       *Database
	mu       sync.RWMutex
	accesses map[string]*packageAccess // key: package name
}

// packageAccess tracks detailed access information for a package.
type packageAccess struct {
	totalCount    uint64          // Total access count
	accessedFiles map[string]bool // Set of files that were accessed
}

// NewMapper creates a mapper initialized with the parsed database and empty access tracking.
func NewMapper(db *Database) *Mapper {
	return &Mapper{
		db:       db,
		accesses: make(map[string]*packageAccess),
	}
}

// RecordAccess records an access to the given file path.
// If the file belongs to a known package, the access is tracked.
// Thread-safe for concurrent access.
func (m *Mapper) RecordAccess(path string) {
	// Look up package owning the file
	pkgName, found := m.db.FileToPackage[path]
	if !found {
		// File not owned by any package, ignore
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Initialize package access if first time seeing this package
	if _, exists := m.accesses[pkgName]; !exists {
		m.accesses[pkgName] = &packageAccess{
			accessedFiles: make(map[string]bool),
		}
	}

	// Record the access
	m.accesses[pkgName].totalCount++
	m.accesses[pkgName].accessedFiles[path] = true
}

// Stats returns access statistics for all packages in the database.
// Packages with zero accesses are included in the results.
// Results are sorted by package name for consistency.
func (m *Mapper) Stats() []PackageStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make([]PackageStats, 0, len(m.db.Packages))

	// Iterate through all packages in database
	for pkgName, pkg := range m.db.Packages {
		stat := PackageStats{
			Name:       pkg.Name,
			Version:    pkg.Version,
			TotalFiles: len(pkg.Files),
		}

		// Add access information if package was accessed
		if access, accessed := m.accesses[pkgName]; accessed {
			stat.AccessedFiles = len(access.accessedFiles)
			stat.AccessCount = access.totalCount
		}
		// Otherwise AccessedFiles and AccessCount remain 0

		stats = append(stats, stat)
	}

	// Sort by package name for consistent output
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].Name < stats[j].Name
	})

	return stats
}
