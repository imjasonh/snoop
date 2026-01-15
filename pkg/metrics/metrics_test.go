package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.EventsReceived == nil {
		t.Error("EventsReceived is nil")
	}
	if m.EventsProcessed == nil {
		t.Error("EventsProcessed is nil")
	}
	if m.EventsExcluded == nil {
		t.Error("EventsExcluded is nil")
	}
	if m.EventsDuplicate == nil {
		t.Error("EventsDuplicate is nil")
	}
	if m.UniqueFiles == nil {
		t.Error("UniqueFiles is nil")
	}
	if m.ReportWrites == nil {
		t.Error("ReportWrites is nil")
	}
	if m.ReportWriteErrors == nil {
		t.Error("ReportWriteErrors is nil")
	}
	if m.registry == nil {
		t.Error("registry is nil")
	}
}

func TestMetricsHandler(t *testing.T) {
	m := New()

	// Increment some counters
	m.EventsReceived.Inc()
	m.EventsReceived.Inc()
	m.EventsProcessed.Inc()
	m.EventsExcluded.Inc()
	m.EventsDuplicate.Inc()
	m.UniqueFiles.Set(42)
	m.ReportWrites.Inc()

	// Create test server with metrics handler
	server := httptest.NewServer(m.Handler())
	defer server.Close()

	// Fetch metrics
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to fetch metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	content := string(body)

	// Verify metrics are present
	for _, tt := range []struct {
		desc   string
		metric string
		value  string
	}{{
		desc:   "events received counter",
		metric: "snoop_events_received_total",
		value:  "2",
	}, {
		desc:   "events processed counter",
		metric: "snoop_events_processed_total",
		value:  "1",
	}, {
		desc:   "events excluded counter",
		metric: "snoop_events_excluded_total",
		value:  "1",
	}, {
		desc:   "events duplicate counter",
		metric: "snoop_events_duplicate_total",
		value:  "1",
	}, {
		desc:   "unique files gauge",
		metric: "snoop_unique_files",
		value:  "42",
	}, {
		desc:   "report writes counter",
		metric: "snoop_report_writes_total",
		value:  "1",
	}, {
		desc:   "report write errors counter",
		metric: "snoop_report_write_errors_total",
		value:  "0",
	}} {
		t.Run(tt.desc, func(t *testing.T) {
			// Look for the metric line with its value
			expectedLine := tt.metric + " " + tt.value
			if !strings.Contains(content, expectedLine) {
				t.Errorf("Expected metric line %q not found in output:\n%s", expectedLine, content)
			}
		})
	}
}

func TestMetricsRegistry(t *testing.T) {
	m := New()
	if m.Registry() == nil {
		t.Error("Registry() returned nil")
	}
	if m.Registry() != m.registry {
		t.Error("Registry() does not return the internal registry")
	}
}

func TestProcessMetricsIncluded(t *testing.T) {
	m := New()

	server := httptest.NewServer(m.Handler())
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to fetch metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	content := string(body)

	// Verify process metrics are included (from ProcessCollector)
	if !strings.Contains(content, "process_") {
		t.Error("Process metrics not found in output")
	}

	// Verify Go runtime metrics are included (from GoCollector)
	if !strings.Contains(content, "go_") {
		t.Error("Go runtime metrics not found in output")
	}
}
