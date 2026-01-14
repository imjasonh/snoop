// Package metrics provides Prometheus metrics for snoop.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for snoop.
type Metrics struct {
	EventsReceived  prometheus.Counter
	EventsProcessed prometheus.Counter
	EventsExcluded  prometheus.Counter
	EventsDuplicate prometheus.Counter
	EventsDropped   prometheus.Counter
	UniqueFiles     prometheus.Gauge

	ReportWrites      prometheus.Counter
	ReportWriteErrors prometheus.Counter

	registry *prometheus.Registry
}

// New creates a new Metrics instance with all metrics registered.
func New() *Metrics {
	registry := prometheus.NewRegistry()

	m := &Metrics{
		EventsReceived: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "snoop_events_received_total",
			Help: "Total number of file access events received from eBPF.",
		}),
		EventsProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "snoop_events_processed_total",
			Help: "Total number of events that resulted in new unique file paths.",
		}),
		EventsExcluded: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "snoop_events_excluded_total",
			Help: "Total number of events filtered by path exclusion rules.",
		}),
		EventsDuplicate: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "snoop_events_duplicate_total",
			Help: "Total number of events for already-seen file paths.",
		}),
		EventsDropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "snoop_events_dropped_total",
			Help: "Total number of events dropped due to ring buffer overflow.",
		}),
		UniqueFiles: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "snoop_unique_files",
			Help: "Current number of unique files recorded.",
		}),
		ReportWrites: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "snoop_report_writes_total",
			Help: "Total number of successful report writes.",
		}),
		ReportWriteErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "snoop_report_write_errors_total",
			Help: "Total number of failed report writes.",
		}),
		registry: registry,
	}

	// Register all metrics
	registry.MustRegister(
		m.EventsReceived,
		m.EventsProcessed,
		m.EventsExcluded,
		m.EventsDuplicate,
		m.EventsDropped,
		m.UniqueFiles,
		m.ReportWrites,
		m.ReportWriteErrors,
	)

	// Register default process metrics (CPU, memory, etc.)
	registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	registry.MustRegister(prometheus.NewGoCollector())

	return m
}

// Handler returns an HTTP handler for the /metrics endpoint.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// Registry returns the Prometheus registry for custom handlers.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}
