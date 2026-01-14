package processor

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
)

func TestNewProcessor(t *testing.T) {
	ctx := context.Background()

	t.Run("with nil exclusions uses defaults", func(t *testing.T) {
		p := NewProcessor(ctx, nil, 0)
		if len(p.excluded) != len(DefaultExclusions()) {
			t.Errorf("expected default exclusions, got %v", p.excluded)
		}
	})

	t.Run("with custom exclusions", func(t *testing.T) {
		exclusions := []string{"/tmp/", "/var/"}
		p := NewProcessor(ctx, exclusions, 0)
		if len(p.excluded) != 2 {
			t.Errorf("expected 2 exclusions, got %d", len(p.excluded))
		}
	})

	t.Run("with bounded cache", func(t *testing.T) {
		p := NewProcessor(ctx, nil, 100)
		if p.seen.maxSize != 100 {
			t.Errorf("expected maxSize=100, got %d", p.seen.maxSize)
		}
	})
}

func TestProcessorProcess(t *testing.T) {
	for _, tt := range []struct {
		desc       string
		path       string
		wantPath   string
		wantResult ProcessResult
	}{{
		desc:       "normal absolute path",
		path:       "/etc/passwd",
		wantPath:   "/etc/passwd",
		wantResult: ResultNew,
	}, {
		desc:       "path with dots normalized",
		path:       "/etc/./nginx/../passwd",
		wantPath:   "/etc/passwd",
		wantResult: ResultNew,
	}, {
		desc:       "proc path excluded",
		path:       "/proc/self/status",
		wantPath:   "/proc/self/status",
		wantResult: ResultExcluded,
	}, {
		desc:       "sys path excluded",
		path:       "/sys/kernel/debug",
		wantPath:   "/sys/kernel/debug",
		wantResult: ResultExcluded,
	}, {
		desc:       "dev path excluded",
		path:       "/dev/null",
		wantPath:   "/dev/null",
		wantResult: ResultExcluded,
	}, {
		desc:       "empty path",
		path:       "",
		wantPath:   "",
		wantResult: ResultEmpty,
	}} {
		t.Run(tt.desc, func(t *testing.T) {
			ctx := context.Background()
			p := NewProcessor(ctx, nil, 0)
			event := &Event{
				PID:  1234,
				Path: tt.path,
			}

			gotPath, gotResult := p.Process(event)
			if gotPath != tt.wantPath {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPath)
			}
			if gotResult != tt.wantResult {
				t.Errorf("result = %v, want %v", gotResult, tt.wantResult)
			}
		})
	}
}

func TestProcessorDeduplication(t *testing.T) {
	ctx := context.Background()
	p := NewProcessor(ctx, nil, 0)

	// First access should be new
	event := &Event{PID: 1234, Path: "/etc/passwd"}
	_, result := p.Process(event)
	if result != ResultNew {
		t.Errorf("first access: got %v, want ResultNew", result)
	}

	// Second access should be duplicate
	_, result = p.Process(event)
	if result != ResultDuplicate {
		t.Errorf("second access: got %v, want ResultDuplicate", result)
	}

	// Third access should still be duplicate
	_, result = p.Process(event)
	if result != ResultDuplicate {
		t.Errorf("third access: got %v, want ResultDuplicate", result)
	}

	// Different path should be new
	event2 := &Event{PID: 1234, Path: "/etc/hostname"}
	_, result = p.Process(event2)
	if result != ResultNew {
		t.Errorf("different path: got %v, want ResultNew", result)
	}
}

func TestProcessorDeduplicationNormalized(t *testing.T) {
	ctx := context.Background()
	p := NewProcessor(ctx, nil, 0)

	// Access with dots
	event1 := &Event{PID: 1234, Path: "/etc/./passwd"}
	_, result := p.Process(event1)
	if result != ResultNew {
		t.Errorf("first access: got %v, want ResultNew", result)
	}

	// Access same file via different path
	event2 := &Event{PID: 1234, Path: "/etc/nginx/../passwd"}
	_, result = p.Process(event2)
	if result != ResultDuplicate {
		t.Errorf("normalized duplicate: got %v, want ResultDuplicate", result)
	}

	// Should only have one unique file
	if p.UniqueFileCount() != 1 {
		t.Errorf("unique files = %d, want 1", p.UniqueFileCount())
	}
}

func TestProcessorFiles(t *testing.T) {
	ctx := context.Background()
	p := NewProcessor(ctx, nil, 0)

	paths := []string{"/etc/passwd", "/usr/bin/bash", "/lib/libc.so.6"}
	for _, path := range paths {
		p.Process(&Event{PID: 1234, Path: path})
	}

	files := p.Files()
	sort.Strings(files)
	sort.Strings(paths)

	if len(files) != len(paths) {
		t.Fatalf("files count = %d, want %d", len(files), len(paths))
	}

	for i, f := range files {
		if f != paths[i] {
			t.Errorf("file[%d] = %q, want %q", i, f, paths[i])
		}
	}
}

func TestProcessorStats(t *testing.T) {
	ctx := context.Background()
	p := NewProcessor(ctx, nil, 0)

	// Process various events
	p.Process(&Event{PID: 1234, Path: "/etc/passwd"})       // new
	p.Process(&Event{PID: 1234, Path: "/etc/passwd"})       // duplicate
	p.Process(&Event{PID: 1234, Path: "/etc/hostname"})     // new
	p.Process(&Event{PID: 1234, Path: "/proc/self/status"}) // excluded
	p.Process(&Event{PID: 1234, Path: ""})                  // empty

	stats := p.Stats()

	if stats.EventsReceived != 5 {
		t.Errorf("EventsReceived = %d, want 5", stats.EventsReceived)
	}
	if stats.EventsProcessed != 2 {
		t.Errorf("EventsProcessed = %d, want 2", stats.EventsProcessed)
	}
	if stats.EventsDuplicate != 1 {
		t.Errorf("EventsDuplicate = %d, want 1", stats.EventsDuplicate)
	}
	if stats.EventsExcluded != 1 {
		t.Errorf("EventsExcluded = %d, want 1", stats.EventsExcluded)
	}
	if stats.UniqueFiles != 2 {
		t.Errorf("UniqueFiles = %d, want 2", stats.UniqueFiles)
	}
}

func TestProcessorReset(t *testing.T) {
	ctx := context.Background()
	p := NewProcessor(ctx, nil, 0)

	p.Process(&Event{PID: 1234, Path: "/etc/passwd"})
	p.Process(&Event{PID: 1234, Path: "/etc/hostname"})

	if p.UniqueFileCount() != 2 {
		t.Fatalf("before reset: unique files = %d, want 2", p.UniqueFileCount())
	}

	p.Reset()

	if p.UniqueFileCount() != 0 {
		t.Errorf("after reset: unique files = %d, want 0", p.UniqueFileCount())
	}

	stats := p.Stats()
	if stats.EventsReceived != 0 {
		t.Errorf("after reset: EventsReceived = %d, want 0", stats.EventsReceived)
	}
}

func TestProcessorConcurrency(t *testing.T) {
	ctx := context.Background()
	p := NewProcessor(ctx, nil, 0)
	var wg sync.WaitGroup

	// Simulate concurrent access from multiple goroutines
	paths := []string{
		"/etc/passwd",
		"/usr/bin/bash",
		"/lib/libc.so.6",
		"/etc/hostname",
		"/usr/lib/libssl.so",
	}

	// Run 10 goroutines, each processing all paths 100 times
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				for _, path := range paths {
					p.Process(&Event{PID: 1234, Path: path})
				}
			}
		}()
	}

	wg.Wait()

	// Should have exactly 5 unique files despite concurrent access
	if p.UniqueFileCount() != 5 {
		t.Errorf("unique files = %d, want 5", p.UniqueFileCount())
	}

	stats := p.Stats()
	// 10 goroutines * 100 iterations * 5 paths = 5000 events
	if stats.EventsReceived != 5000 {
		t.Errorf("EventsReceived = %d, want 5000", stats.EventsReceived)
	}
}

func TestProcessorCustomExclusions(t *testing.T) {
	ctx := context.Background()
	// Test with custom exclusions that don't include defaults
	p := NewProcessor(ctx, []string{"/tmp/", "/custom/"}, 0)

	// Default exclusions should NOT apply
	_, result := p.Process(&Event{PID: 1234, Path: "/proc/self/status"})
	if result != ResultNew {
		t.Errorf("/proc path: got %v, want ResultNew (custom exclusions)", result)
	}

	// Custom exclusions SHOULD apply
	_, result = p.Process(&Event{PID: 1234, Path: "/tmp/file.txt"})
	if result != ResultExcluded {
		t.Errorf("/tmp path: got %v, want ResultExcluded", result)
	}

	_, result = p.Process(&Event{PID: 1234, Path: "/custom/data"})
	if result != ResultExcluded {
		t.Errorf("/custom path: got %v, want ResultExcluded", result)
	}
}

func TestProcessorBoundedCache(t *testing.T) {
	ctx := context.Background()
	// Create processor with max 3 unique files
	p := NewProcessor(ctx, []string{}, 3)

	// Add 3 files - should all be new
	for i := 1; i <= 3; i++ {
		path := fmt.Sprintf("/file%d", i)
		_, result := p.Process(&Event{PID: 1234, Path: path})
		if result != ResultNew {
			t.Errorf("file %d: got %v, want ResultNew", i, result)
		}
	}

	stats := p.Stats()
	if stats.UniqueFiles != 3 {
		t.Errorf("unique files = %d, want 3", stats.UniqueFiles)
	}
	if stats.EventsEvicted != 0 {
		t.Errorf("evicted = %d, want 0", stats.EventsEvicted)
	}

	// Add 4th file - should evict oldest (file1)
	_, result := p.Process(&Event{PID: 1234, Path: "/file4"})
	if result != ResultNew {
		t.Errorf("file4: got %v, want ResultNew", result)
	}

	stats = p.Stats()
	if stats.UniqueFiles != 3 {
		t.Errorf("unique files after eviction = %d, want 3", stats.UniqueFiles)
	}
	if stats.EventsEvicted != 1 {
		t.Errorf("evicted = %d, want 1", stats.EventsEvicted)
	}

	// file1 should now be treated as new (was evicted)
	_, result = p.Process(&Event{PID: 1234, Path: "/file1"})
	if result != ResultNew {
		t.Errorf("evicted file1: got %v, want ResultNew", result)
	}

	stats = p.Stats()
	if stats.EventsEvicted != 2 {
		t.Errorf("evicted after re-add = %d, want 2", stats.EventsEvicted)
	}
}

func TestProcessorBoundedCacheWithHighLoad(t *testing.T) {
	ctx := context.Background()
	// Create processor with max 10 unique files
	p := NewProcessor(ctx, []string{}, 10)

	// Add 100 unique files
	for i := 0; i < 100; i++ {
		path := fmt.Sprintf("/file%d", i)
		p.Process(&Event{PID: 1234, Path: path})
	}

	stats := p.Stats()
	// Should only retain 10 files
	if stats.UniqueFiles != 10 {
		t.Errorf("unique files = %d, want 10", stats.UniqueFiles)
	}
	// Should have evicted 90 files
	if stats.EventsEvicted != 90 {
		t.Errorf("evicted = %d, want 90", stats.EventsEvicted)
	}
	// 100 events received, all processed
	if stats.EventsProcessed != 100 {
		t.Errorf("processed = %d, want 100", stats.EventsProcessed)
	}
}

func TestProcessorUnboundedVsBounded(t *testing.T) {
	ctx := context.Background()

	// Unbounded processor
	pUnbounded := NewProcessor(ctx, []string{}, 0)
	// Bounded processor
	pBounded := NewProcessor(ctx, []string{}, 5)

	// Add 20 unique files to both
	for i := 0; i < 20; i++ {
		path := fmt.Sprintf("/file%d", i)
		pUnbounded.Process(&Event{PID: 1234, Path: path})
		pBounded.Process(&Event{PID: 1234, Path: path})
	}

	unboundedStats := pUnbounded.Stats()
	boundedStats := pBounded.Stats()

	// Unbounded should have all 20 files
	if unboundedStats.UniqueFiles != 20 {
		t.Errorf("unbounded unique files = %d, want 20", unboundedStats.UniqueFiles)
	}
	if unboundedStats.EventsEvicted != 0 {
		t.Errorf("unbounded evicted = %d, want 0", unboundedStats.EventsEvicted)
	}

	// Bounded should have only 5 files
	if boundedStats.UniqueFiles != 5 {
		t.Errorf("bounded unique files = %d, want 5", boundedStats.UniqueFiles)
	}
	if boundedStats.EventsEvicted != 15 {
		t.Errorf("bounded evicted = %d, want 15", boundedStats.EventsEvicted)
	}
}
