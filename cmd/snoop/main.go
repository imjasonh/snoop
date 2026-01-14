//go:build linux

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chainguard-dev/clog"
	"github.com/imjasonh/snoop/pkg/cgroup"
	"github.com/imjasonh/snoop/pkg/config"
	"github.com/imjasonh/snoop/pkg/ebpf"
	"github.com/imjasonh/snoop/pkg/health"
	"github.com/imjasonh/snoop/pkg/metrics"
	"github.com/imjasonh/snoop/pkg/processor"
	"github.com/imjasonh/snoop/pkg/reporter"
)

func main() {
	var (
		cgroupPath     string
		reportPath     string
		reportInterval time.Duration
		excludePaths   string
		imageRef       string
		containerID    string
		metricsAddr    string
		logLevel       string
		maxUniqueFiles int
	)

	flag.StringVar(&cgroupPath, "cgroup", "", "Cgroup path to trace (e.g., /system.slice/docker-abc123.scope)")
	flag.StringVar(&reportPath, "report", "/data/snoop-report.json", "Path to write the JSON report")
	flag.DurationVar(&reportInterval, "interval", 30*time.Second, "Interval between report writes")
	flag.StringVar(&excludePaths, "exclude", "/proc/,/sys/,/dev/", "Comma-separated path prefixes to exclude")
	flag.StringVar(&imageRef, "image", "", "Image reference for report metadata")
	flag.StringVar(&containerID, "container-id", "", "Container ID for report metadata")
	flag.StringVar(&metricsAddr, "metrics-addr", ":9090", "Address for Prometheus metrics endpoint (empty to disable)")
	flag.StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	flag.IntVar(&maxUniqueFiles, "max-unique-files", 0, "Maximum unique files to track (0 = unbounded)")
	flag.Parse()

	// Build configuration from flags
	cfg := &config.Config{
		CgroupPath:     cgroupPath,
		ReportPath:     reportPath,
		ReportInterval: reportInterval,
		ExcludePaths:   config.ParseExcludePaths(excludePaths),
		ImageRef:       imageRef,
		ContainerID:    containerID,
		MetricsAddr:    metricsAddr,
		LogLevel:       logLevel,
		MaxUniqueFiles: maxUniqueFiles,
	}

	// Initialize logging context
	ctx := clog.WithLogger(context.Background(), clog.New(clog.ParseLevel(cfg.LogLevel)))

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		clog.FromContext(ctx).Fatalf("Configuration validation failed: %v", err)
	}

	if err := run(ctx, cfg); err != nil {
		clog.FromContext(ctx).Fatalf("Fatal error: %v", err)
	}
}

func run(ctx context.Context, cfg *config.Config) error {
	log := clog.FromContext(ctx)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Info("Received shutdown signal")
		cancel()
	}()

	// Initialize metrics and health checker
	m := metrics.New()
	healthChecker := health.New()

	// Start metrics and health server if address is provided
	if cfg.MetricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", m.Handler())
		mux.Handle("/healthz", healthChecker.Handler())
		server := &http.Server{
			Addr:    cfg.MetricsAddr,
			Handler: mux,
		}
		go func() {
			log.Infof("Starting metrics and health server on %s", cfg.MetricsAddr)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Errorf("Metrics server error: %v", err)
			}
		}()
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			server.Shutdown(shutdownCtx)
		}()
	}

	// Create and load the eBPF probe
	log.Info("Loading eBPF program")
	probe, err := ebpf.NewProbe(ctx)
	if err != nil {
		return fmt.Errorf("creating probe: %w", err)
	}
	defer probe.Close()

	log.Info("eBPF program loaded successfully")
	healthChecker.SetEBPFLoaded()

	// Add cgroup to trace
	cgroupID, err := cgroup.GetCgroupIDByPath(cfg.CgroupPath)
	if err != nil {
		return fmt.Errorf("getting cgroup ID: %w", err)
	}
	log.Infof("Tracing cgroup: %s (ID: %d)", cfg.CgroupPath, cgroupID)
	if err := probe.AddTracedCgroup(cgroupID); err != nil {
		return fmt.Errorf("adding traced cgroup: %w", err)
	}

	// Create processor and reporter
	proc := processor.NewProcessor(ctx, cfg.ExcludePaths, cfg.MaxUniqueFiles)
	rep := reporter.NewFileReporter(ctx, cfg.ReportPath)

	startedAt := time.Now()
	log.Infof("Writing reports to: %s (interval: %s)", cfg.ReportPath, cfg.ReportInterval)

	// Track last seen drops and evictions count for computing deltas
	var lastDrops uint64
	var lastEvicted uint64

	// Start periodic report writer
	reportTicker := time.NewTicker(cfg.ReportInterval)
	defer reportTicker.Stop()

	writeReport := func() {
		stats := proc.Stats()
		drops, err := probe.Drops()
		if err != nil {
			log.Warnf("Failed to read drops counter: %v", err)
			drops = 0
		}

		// Update the drops counter metric with the delta
		if drops > lastDrops {
			delta := drops - lastDrops
			m.EventsDropped.Add(float64(delta))
			if delta > 0 {
				log.Warnf("Ring buffer overflow: %d events dropped since last report", delta)
			}
			lastDrops = drops
		}

		// Update the evictions counter metric with the delta
		if stats.EventsEvicted > lastEvicted {
			delta := stats.EventsEvicted - lastEvicted
			m.EventsEvicted.Add(float64(delta))
			if delta > 0 {
				log.Warnf("Deduplication cache eviction: %d file paths evicted since last report", delta)
			}
			lastEvicted = stats.EventsEvicted
		}

		report := &reporter.Report{
			ContainerID:   cfg.ContainerID,
			ImageRef:      cfg.ImageRef,
			StartedAt:     startedAt,
			Files:         proc.Files(),
			TotalEvents:   stats.EventsReceived,
			DroppedEvents: drops,
		}
		if err := rep.Update(ctx, report); err != nil {
			log.Errorf("Error writing report: %v", err)
			m.ReportWriteErrors.Inc()
		} else {
			log.Infof("Report written: %d unique files, %d events processed, %d dropped, %d evicted",
				stats.UniqueFiles, stats.EventsProcessed, drops, stats.EventsEvicted)
			m.ReportWrites.Inc()
			healthChecker.RecordReportWritten()
		}
		// Update gauge for unique files count
		m.UniqueFiles.Set(float64(stats.UniqueFiles))
	}

	// Read and process events
	log.Info("Waiting for events (press Ctrl+C to exit)")
	for {
		select {
		case <-ctx.Done():
			// Graceful shutdown: write final report
			log.Info("Writing final report")
			writeReport()
			return nil

		case <-reportTicker.C:
			writeReport()

		default:
			event, err := probe.ReadEvent(ctx)
			if err != nil {
				if ctx.Err() != nil {
					// Context cancelled, write final report
					log.Info("Writing final report")
					writeReport()
					return nil
				}
				log.Errorf("Error reading event: %v", err)
				continue
			}

			// Convert ebpf.Event to processor.Event
			procEvent := &processor.Event{
				CgroupID:  event.CgroupID,
				PID:       event.PID,
				SyscallNr: event.SyscallNr,
				Path:      event.Path,
			}

			// Update received counter
			m.EventsReceived.Inc()
			healthChecker.RecordEventReceived()

			path, result := proc.Process(procEvent)
			switch result {
			case processor.ResultNew:
				m.EventsProcessed.Inc()
				log.Debugf("New file: %s", path)
			case processor.ResultDuplicate:
				m.EventsDuplicate.Inc()
			case processor.ResultExcluded:
				m.EventsExcluded.Inc()
			}
		}
	}
}
