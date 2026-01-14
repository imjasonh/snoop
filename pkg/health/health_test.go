package health

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthChecker(t *testing.T) {
	for _, tt := range []struct {
		desc        string
		setup       func(*Checker)
		wantHealthy bool
		wantMessage string
	}{
		{
			desc: "newly created checker is unhealthy (eBPF not loaded)",
			setup: func(c *Checker) {
				// No setup - checker is in initial state
			},
			wantHealthy: false,
			wantMessage: "eBPF program not loaded",
		},
		{
			desc: "healthy with eBPF loaded and recent activity",
			setup: func(c *Checker) {
				c.SetEBPFLoaded()
				c.RecordEventReceived()
				c.RecordReportWritten()
			},
			wantHealthy: true,
			wantMessage: "",
		},
		{
			desc: "healthy with eBPF loaded but no events yet (within grace period)",
			setup: func(c *Checker) {
				c.SetEBPFLoaded()
				c.RecordReportWritten()
			},
			wantHealthy: true,
			wantMessage: "",
		},
		{
			desc: "unhealthy when report write stalled",
			setup: func(c *Checker) {
				c.SetEBPFLoaded()
				c.RecordEventReceived()
				// Set last report to 3 minutes ago
				c.mu.Lock()
				c.lastReportWritten = time.Now().Add(-3 * time.Minute)
				c.mu.Unlock()
			},
			wantHealthy: false,
			wantMessage: "report write stalled",
		},
		{
			desc: "warning when no recent events but reports working",
			setup: func(c *Checker) {
				c.SetEBPFLoaded()
				c.RecordReportWritten()
				// Set last event to 6 minutes ago
				c.mu.Lock()
				c.lastEventReceived = time.Now().Add(-6 * time.Minute)
				c.mu.Unlock()
			},
			wantHealthy: true, // Still healthy, just a warning
			wantMessage: "no events received recently (check cgroup filter)",
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			c := New()
			tt.setup(c)

			status := c.Check()

			if status.Healthy != tt.wantHealthy {
				t.Errorf("Healthy: got %v, want %v", status.Healthy, tt.wantHealthy)
			}

			if status.Message != tt.wantMessage {
				t.Errorf("Message: got %q, want %q", status.Message, tt.wantMessage)
			}

			if !status.EBPFLoaded && tt.wantHealthy {
				t.Error("Cannot be healthy without eBPF loaded")
			}
		})
	}
}

func TestHealthHandler(t *testing.T) {
	for _, tt := range []struct {
		desc           string
		setup          func(*Checker)
		wantStatusCode int
	}{
		{
			desc: "returns 503 when unhealthy",
			setup: func(c *Checker) {
				// No setup - checker is unhealthy by default
			},
			wantStatusCode: http.StatusServiceUnavailable,
		},
		{
			desc: "returns 200 when healthy",
			setup: func(c *Checker) {
				c.SetEBPFLoaded()
				c.RecordEventReceived()
				c.RecordReportWritten()
			},
			wantStatusCode: http.StatusOK,
		},
	} {
		t.Run(tt.desc, func(t *testing.T) {
			c := New()
			tt.setup(c)

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()

			c.Handler()(rec, req)

			if rec.Code != tt.wantStatusCode {
				t.Errorf("Status code: got %d, want %d", rec.Code, tt.wantStatusCode)
			}

			// Verify JSON response
			var status Status
			if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			// Verify Content-Type
			if got := rec.Header().Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type: got %q, want %q", got, "application/json")
			}

			// Verify response matches health status
			expectedHealthy := tt.wantStatusCode == http.StatusOK
			if status.Healthy != expectedHealthy {
				t.Errorf("Response healthy field: got %v, want %v", status.Healthy, expectedHealthy)
			}
		})
	}
}

func TestHealthStatus(t *testing.T) {
	c := New()
	c.SetEBPFLoaded()
	c.RecordEventReceived()
	c.RecordReportWritten()

	status := c.Check()

	// Verify all expected fields are present
	if status.Uptime == "" {
		t.Error("Expected uptime to be set")
	}

	if !status.EBPFLoaded {
		t.Error("Expected EBPFLoaded to be true")
	}

	if status.LastEventReceived == "" {
		t.Error("Expected LastEventReceived to be set")
	}

	if status.LastReportWritten == "" {
		t.Error("Expected LastReportWritten to be set")
	}

	if status.SecondsSinceEvent < 0 {
		t.Errorf("Expected non-negative SecondsSinceEvent, got %f", status.SecondsSinceEvent)
	}

	if status.SecondsSinceReport < 0 {
		t.Errorf("Expected non-negative SecondsSinceReport, got %f", status.SecondsSinceReport)
	}
}
