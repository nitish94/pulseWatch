package analysis

import (
	"container/list"
	"fmt"
	"log" // Added log import
	"sync"
	"time"

	"github.com/VividCortex/ewma"
	"github.com/montanaflynn/stats"
	"github.com/nitis/pulseWatch/internal/storage"
	"github.com/nitis/pulseWatch/internal/types"
)

const (
	defaultWindow         = 5 * time.Minute
	defaultTickInterval   = 1 * time.Second
	latencyPercentile     = 95
	errorRateSpikeThreshold = 3.0 // 3x increase
	pruneInterval         = 1 * time.Hour // Prune DB every hour
	maxDBAge              = 7 * 24 * time.Hour // Keep 7 days in DB
)

// Engine is the analysis engine for pulsewatch.
type Engine struct {
	windowDuration time.Duration
	tickInterval   time.Duration
	windows        map[string]time.Duration
	initialScan    bool

	logEntries *list.List
	latencies  []float64
	mu         sync.Mutex
	dirty      bool // New field to track if new logs have been added

	rpsEWMA ewma.MovingAverage

	metrics                types.Metrics
	metricsChan            chan types.Metrics
	doneChan               chan struct{}
	statusCodeDistribution map[string]int
	storage                *storage.Storage
	lastPrune              time.Time
}

// NewEngine creates a new analysis engine.
func NewEngine(dbPath string, initialScan bool) (*Engine, error) {
	fmt.Println("Engine: NewEngine initialScan:", initialScan)
	stor, err := storage.NewStorage(dbPath)
	if err != nil {
		return nil, err
	}

	windows := map[string]time.Duration{
		"1m":  1 * time.Minute,
		"5m":  5 * time.Minute,
		"1h":  1 * time.Hour,
	}

	return &Engine{
		windowDuration: defaultWindow,
		tickInterval:   defaultTickInterval,
		windows:        windows,
		initialScan:    initialScan,
		logEntries:     list.New(),
		rpsEWMA:        ewma.NewMovingAverage(),
		metricsChan:    make(chan types.Metrics),
		doneChan:       make(chan struct{}),
		metrics: types.Metrics{
			Windows:   make(map[string]types.WindowedMetrics),
			Anomalies: []types.Anomaly{},
			StartTime: time.Now(),
		},
		statusCodeDistribution: make(map[string]int),
		storage:                stor,
		dirty:                  false,
		lastPrune:              time.Now(),
	}, nil
}

// Start begins the analysis engine's processing loop.
func (e *Engine) Start(logChan <-chan types.LogEntry) <-chan types.Metrics {
	// Load existing entries from DB
	e.loadExistingEntries()
	go e.processLogs(logChan)
	go e.runTicker()
	return e.metricsChan
}

// Stop halts the analysis engine.
func (e *Engine) Stop() {
	e.storage.Close()
	close(e.doneChan)
}

func (e *Engine) loadExistingEntries() {
	entries, err := e.storage.GetLogEntriesSince(time.Now().Add(-maxDBAge))
	if err != nil {
		log.Printf("Error loading existing entries: %v", err)
		return
	}
	for _, entry := range entries {
		e.addLogEntry(entry)
	}
	log.Printf("Loaded %d existing log entries from DB", len(entries))
}

func (e *Engine) processLogs(logChan <-chan types.LogEntry) {
	log.Println("Engine: processLogs started")
	for {
		select {
		case logEntry, ok := <-logChan:
			if !ok {
				log.Println("Engine: logChan closed, processLogs exiting")
				return
			}
			log.Println("Engine: Received log entry")
			e.addLogEntry(logEntry)
		case <-e.doneChan:
			log.Println("Engine: Context cancelled, processLogs exiting")
			return
		}
	}
}

func (e *Engine) addLogEntry(entry types.LogEntry) {
	fmt.Println("Engine: addLogEntry called")
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	e.logEntries.PushBack(entry)

	// Insert to DB
	if err := e.storage.InsertLogEntry(entry); err != nil {
		log.Printf("Error inserting log entry to DB: %v", err)
	}

	// Add to latencies, but only for successful requests
	if entry.StatusCode < 400 && entry.Latency > 0 {
		e.latencies = append(e.latencies, float64(entry.Latency.Milliseconds()))
	}

	e.dirty = true // Mark as dirty when a new log is added
	fmt.Println("Engine: dirty set to true")

	// Prune old entries
	e.prune(now)
}

func (e *Engine) prune(now time.Time) {
	// Prune log entries
	for front := e.logEntries.Front(); front != nil; front = e.logEntries.Front() {
		if now.Sub(front.Value.(types.LogEntry).Timestamp) > e.windowDuration {
			e.logEntries.Remove(front)
		} else {
			break
		}
	}

	// A bit inefficient to rebuild latencies every time, but simpler for now
	e.latencies = e.latencies[:0]
	for elem := e.logEntries.Front(); elem != nil; elem = elem.Next() {
		entry := elem.Value.(types.LogEntry)
		if entry.StatusCode < 400 && entry.Latency > 0 {
			e.latencies = append(e.latencies, float64(entry.Latency.Milliseconds()))
		}
	}
}

func (e *Engine) pruneDB(now time.Time) {
	olderThan := now.Add(-maxDBAge)
	if err := e.storage.PruneOldEntries(olderThan); err != nil {
		log.Printf("Error pruning DB: %v", err)
	}
}

func (e *Engine) runTicker() {
	ticker := time.NewTicker(e.tickInterval)
	defer ticker.Stop()

	log.Println("Engine: runTicker starting")
	for {
		select {
		case <-ticker.C:
			e.mu.Lock() // Lock to check and modify dirty flag
			if e.dirty {
				e.calculateMetrics()
				e.detectAnomalies()
				e.metricsChan <- e.metrics
				e.dirty = false // Reset dirty flag after sending
				fmt.Println("Engine: Sent metrics to metricsChan.")
			} else {
				fmt.Println("Engine: Ticker fired, but no new logs. Not sending metrics.")
			}

			// Periodic prune
			if time.Since(e.lastPrune) > pruneInterval {
				now := time.Now()
				e.pruneDB(now)
				e.lastPrune = now
			}
			e.mu.Unlock() // Unlock after operations
		case <-e.doneChan:
			log.Println("Engine: Context cancelled, runTicker exiting")
			return
		default:
			// For initial scan, send metrics immediately if dirty
			fmt.Println("Engine: runTicker default, initialScan:", e.initialScan, "dirty:", e.dirty)
			if e.initialScan {
				e.mu.Lock()
				if e.dirty {
					e.calculateMetrics()
					e.detectAnomalies()
					e.metricsChan <- e.metrics
					e.dirty = false
					fmt.Println("Engine: Sent initial metrics to metricsChan.")
				}
				e.mu.Unlock()
			}
		}
	}
}

func (e *Engine) calculateMetrics() {
	fmt.Println("Engine: calculateMetrics called")

	e.metrics.Windows = make(map[string]types.WindowedMetrics)

	if e.initialScan {
		// For initial scan, compute metrics for all entries
		fmt.Println("Engine: Getting all entries for initial scan")
		entries, err := e.storage.GetLogEntriesSince(time.Time{}) // All entries
		if err != nil {
			log.Printf("Error getting all entries: %v", err)
		} else {
			fmt.Printf("Engine: Got %d entries\n", len(entries))
			wm := e.computeWindowedMetrics(entries, 0) // 0 for no RPS calc
			e.metrics.Windows["all"] = wm
			fmt.Println("Engine: Computed metrics for all")
		}
	} else {
		for key, window := range e.windows {
			entries, err := e.storage.GetEntriesInWindow(window)
			if err != nil {
				log.Printf("Error getting entries for window %s: %v", key, err)
				continue
			}

			wm := e.computeWindowedMetrics(entries, window)
			e.metrics.Windows[key] = wm
		}
	}

	// For backward compatibility, keep Anomalies and StartTime
}

func (e *Engine) computeWindowedMetrics(entries []types.LogEntry, window time.Duration) types.WindowedMetrics {
	if len(entries) == 0 {
		return types.WindowedMetrics{
			TopEndpoints:           make(map[string]int),
			StatusCodeDistribution: make(map[string]int),
		}
	}

	var latencies []float64
	topEndpoints := make(map[string]int)
	statusCodeDist := make(map[string]int)
	totalRequests := len(entries)
	totalErrors := 0

	for _, entry := range entries {
		if entry.StatusCode >= 400 {
			totalErrors++
		}
		if entry.Endpoint != "" {
			topEndpoints[entry.Endpoint]++
		}
		if entry.StatusCode < 400 && entry.Latency > 0 {
			latencies = append(latencies, float64(entry.Latency.Milliseconds()))
		}

		statusCodeCategory := func(code int) string {
			switch {
			case code >= 100 && code < 200:
				return "1xx"
			case code >= 200 && code < 300:
				return "2xx"
			case code >= 300 && code < 400:
				return "3xx"
			case code >= 400 && code < 500:
				return "4xx"
			case code >= 500 && code < 600:
				return "5xx"
			default:
				return "Other"
			}
		}(entry.StatusCode)
		statusCodeDist[statusCodeCategory]++
	}

	rps := 0.0
	if window > 0 {
		rps = float64(totalRequests) / window.Seconds()
	}
	errorRate := 0.0
	if totalRequests > 0 {
		errorRate = (float64(totalErrors) / float64(totalRequests)) * 100
	}

	var p50, p90, p95, p99 time.Duration
	if len(latencies) > 0 {
		p50v, _ := stats.Percentile(latencies, 50)
		p90v, _ := stats.Percentile(latencies, 90)
		p95v, _ := stats.Percentile(latencies, 95)
		p99v, _ := stats.Percentile(latencies, 99)
		p50 = time.Duration(p50v) * time.Millisecond
		p90 = time.Duration(p90v) * time.Millisecond
		p95 = time.Duration(p95v) * time.Millisecond
		p99 = time.Duration(p99v) * time.Millisecond
	}

	return types.WindowedMetrics{
		RPS:                    rps,
		ErrorRate:              errorRate,
		P50Latency:             p50,
		P90Latency:             p90,
		P95Latency:             p95,
		P99Latency:             p99,
		TopEndpoints:           topEndpoints,
		TotalRequests:          totalRequests,
		TotalErrors:            totalErrors,
		StatusCodeDistribution: statusCodeDist,
	}
}

func (e *Engine) detectAnomalies() {
	// Simple anomaly detection for now
	// Use 1h window for detection
	wm, ok := e.metrics.Windows["1h"]
	if !ok {
		return
	}

	// Error Rate Spike
	// This is a placeholder. A real implementation would compare against a baseline.
	if wm.ErrorRate > 10.0 && len(e.metrics.Anomalies) == 0 { // Add anomaly only once for now
		e.metrics.Anomalies = append(e.metrics.Anomalies, types.Anomaly{
			Timestamp: time.Now(),
			Type:      "High Error Rate",
			Message:   "Error rate is above 10%",
		})
	}

	// Latency Jump
	if wm.P95Latency > 1*time.Second && len(e.metrics.Anomalies) <= 1 { // Add anomaly only once for now
		e.metrics.Anomalies = append(e.metrics.Anomalies, types.Anomaly{
			Timestamp: time.Now(),
			Type:      "High Latency",
			Message:   "P95 latency is over 1 second",
		})
	}
}

