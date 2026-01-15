// Package health provides health checking functionality for snoop.
package health

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Checker tracks the health status of various snoop components.
type Checker struct {
	mu                sync.RWMutex
	ebpfLoaded        bool
	lastEventReceived time.Time
	lastReportWritten time.Time
	startTime         time.Time
}

// New creates a new health checker.
func New() *Checker {
	return &Checker{
		startTime: time.Now(),
	}
}

// SetEBPFLoaded marks the eBPF program as successfully loaded.
func (c *Checker) SetEBPFLoaded() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ebpfLoaded = true
}

// RecordEventReceived updates the timestamp of the last event received.
func (c *Checker) RecordEventReceived() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastEventReceived = time.Now()
}

// RecordReportWritten updates the timestamp of the last successful report write.
func (c *Checker) RecordReportWritten() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastReportWritten = time.Now()
}

// Status represents the current health status.
type Status struct {
	Healthy            bool    `json:"healthy"`
	Uptime             string  `json:"uptime"`
	EBPFLoaded         bool    `json:"ebpf_loaded"`
	LastEventReceived  string  `json:"last_event_received,omitempty"`
	LastReportWritten  string  `json:"last_report_written,omitempty"`
	SecondsSinceEvent  float64 `json:"seconds_since_event,omitempty"`
	SecondsSinceReport float64 `json:"seconds_since_report,omitempty"`
	Message            string  `json:"message,omitempty"`
}

// Check returns the current health status.
// It considers the service healthy if:
// - eBPF program is loaded
// - Events have been received (or it's been less than 5 minutes since start)
// - Reports have been written (or it's been less than 5 minutes since start)
func (c *Checker) Check() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()
	uptime := now.Sub(c.startTime)

	status := Status{
		Healthy:    true,
		Uptime:     uptime.Round(time.Second).String(),
		EBPFLoaded: c.ebpfLoaded,
	}

	// Check eBPF loaded
	if !c.ebpfLoaded {
		status.Healthy = false
		status.Message = "eBPF program not loaded"
		return status
	}

	// Check event reception (but allow grace period after startup)
	if !c.lastEventReceived.IsZero() {
		timeSinceEvent := now.Sub(c.lastEventReceived)
		status.SecondsSinceEvent = timeSinceEvent.Seconds()
		status.LastEventReceived = c.lastEventReceived.Format(time.RFC3339)

		// Alert if no events in 5 minutes (might indicate cgroup filter issue)
		if timeSinceEvent > 5*time.Minute {
			status.Message = "no events received recently (check cgroup filter)"
		}
	} else if uptime > 5*time.Minute {
		// No events at all after 5 minutes of uptime
		status.Message = "no events received yet (check cgroup filter)"
	}

	// Check report writes
	if !c.lastReportWritten.IsZero() {
		timeSinceReport := now.Sub(c.lastReportWritten)
		status.SecondsSinceReport = timeSinceReport.Seconds()
		status.LastReportWritten = c.lastReportWritten.Format(time.RFC3339)

		// Alert if no report written in 2 minutes (should write every 30s by default)
		if timeSinceReport > 2*time.Minute {
			status.Healthy = false
			if status.Message != "" {
				status.Message += "; "
			}
			status.Message += "report write stalled"
		}
	} else if uptime > 2*time.Minute {
		// No reports at all after 2 minutes of uptime
		status.Healthy = false
		if status.Message != "" {
			status.Message += "; "
		}
		status.Message += "no reports written yet"
	}

	return status
}

// Handler returns an HTTP handler for the /healthz endpoint.
// Returns 200 OK if healthy, 503 Service Unavailable if unhealthy.
func (c *Checker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := c.Check()

		w.Header().Set("Content-Type", "application/json")
		if !status.Healthy {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		_ = json.NewEncoder(w).Encode(status)
	}
}
