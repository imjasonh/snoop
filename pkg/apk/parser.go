// Package apk handles parsing of APK package databases and mapping file accesses to packages.
package apk

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Package represents an installed APK package.
type Package struct {
	Name    string
	Version string
	Files   []string // All files owned by this package
}

// Database holds the parsed APK installed database.
type Database struct {
	Packages      map[string]*Package // key: package name
	FileToPackage map[string]string   // key: file path, value: package name
}

// ParseDatabase reads and parses the APK installed database from the given path.
// The database is typically located at /lib/apk/db/installed in Alpine/Wolfi containers.
func ParseDatabase(path string) (*Database, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open APK database: %w", err)
	}
	defer func() { _ = f.Close() }()

	db := &Database{
		Packages:      make(map[string]*Package),
		FileToPackage: make(map[string]string),
	}

	scanner := bufio.NewScanner(f)
	lineNum := 0

	var currentPkg *Package
	var currentPkgName string

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Blank line separates packages
		if line == "" {
			if currentPkg != nil {
				db.Packages[currentPkgName] = currentPkg
				currentPkg = nil
				currentPkgName = ""
			}
			continue
		}

		// Lines must be in key:value format
		if len(line) < 2 || line[1] != ':' {
			// Skip malformed lines
			continue
		}

		key := line[0:1]
		value := line[2:]

		switch key {
		case "P":
			// Start of new package
			if currentPkg != nil {
				// Save previous package before starting new one
				db.Packages[currentPkgName] = currentPkg
			}
			currentPkgName = value
			currentPkg = &Package{
				Name:  value,
				Files: []string{},
			}
		case "V":
			// Version
			if currentPkg != nil {
				currentPkg.Version = value
			}
		case "F":
			// File path (relative in APK database, we prefix with /)
			if currentPkg != nil {
				// Normalize to absolute path
				filePath := value
				if !strings.HasPrefix(filePath, "/") {
					filePath = "/" + filePath
				}
				currentPkg.Files = append(currentPkg.Files, filePath)

				// Map file to package (use first occurrence if duplicate)
				if existingPkg, exists := db.FileToPackage[filePath]; exists {
					// File already claimed by another package, skip
					_ = existingPkg // Avoid unused variable warning
				} else {
					db.FileToPackage[filePath] = currentPkgName
				}
			}
		}
	}

	// Don't forget the last package
	if currentPkg != nil {
		db.Packages[currentPkgName] = currentPkg
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading APK database: %w", err)
	}

	if len(db.Packages) == 0 {
		return nil, fmt.Errorf("APK database is empty or contains no valid packages")
	}

	return db, nil
}
