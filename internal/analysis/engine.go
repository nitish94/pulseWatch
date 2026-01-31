package analysis

import (
	"container/list"
	"fmt"
	"log"
	"math"
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
	maxMetricsHistory     = 20 // Keep last 20 metrics for trends
)

// Engine is the analysis engine for pulsewatch.
type Engine struct {
	windowDuration time.Duration
	tickInterval   time.Duration
	windows        map[string]time.Duration
	initialScan    bool
	customMetrics  []types.CustomMetric

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
	metricsHistory         []types.TrendPoint
	rpsHistory             []float64
	errorRateHistory       []float64
	latencyHistory         []float64
}

// NewEngine creates a new analysis engine.
func NewEngine(dbPath string, initialScan bool, customMetrics []types.CustomMetric) (*Engine, error) {
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
		metricsHistory:         make([]types.TrendPoint, 0, maxMetricsHistory),
		rpsHistory:             make([]float64, 0, maxMetricsHistory),
		errorRateHistory:       make([]float64, 0, maxMetricsHistory),
		latencyHistory:         make([]float64, 0, maxMetricsHistory),
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
	// entries, err := e.storage.GetLogEntriesSince(time.Now().Add(-maxDBAge))
	// if err != nil {
	// 	log.Printf("Error loading existing entries: %v", err)
	// 	return
	// }
	// for _, entry := range entries {
	// 	e.addLogEntry(entry)
	// }

}

func (e *Engine) processLogs(logChan <-chan types.LogEntry) {
	for {
		select {
		case logEntry, ok := <-logChan:
			if !ok {
				if e.initialScan {
					e.calculateMetrics()
					e.detectAnomalies()
					// Append to history
					wm, ok := e.metrics.Windows["all"]
					if !ok {
						wm, ok = e.metrics.Windows["1m"]
					}
					if ok {
						tp := types.TrendPoint{
							RPS:       wm.RPS,
							P95Latency: wm.P95Latency,
							ErrorRate: wm.ErrorRate,
						}
						e.metricsHistory = append(e.metricsHistory, tp)
						if len(e.metricsHistory) > maxMetricsHistory {
							e.metricsHistory = e.metricsHistory[1:]
						}
						e.rpsHistory = append(e.rpsHistory, wm.RPS)
						if len(e.rpsHistory) > maxMetricsHistory {
							e.rpsHistory = e.rpsHistory[1:]
						}
						e.errorRateHistory = append(e.errorRateHistory, wm.ErrorRate)
						if len(e.errorRateHistory) > maxMetricsHistory {
							e.errorRateHistory = e.errorRateHistory[1:]
						}
						e.latencyHistory = append(e.latencyHistory, float64(wm.P95Latency.Milliseconds()))
						if len(e.latencyHistory) > maxMetricsHistory {
							e.latencyHistory = e.latencyHistory[1:]
						}
					}
					e.metrics.TrendHistory = make([]types.TrendPoint, len(e.metricsHistory))
					copy(e.metrics.TrendHistory, e.metricsHistory)
					e.metricsChan <- e.metrics
				}
				return
			}
			e.addLogEntry(logEntry)
		case <-e.doneChan:
			return
		}
	}
}

func (e *Engine) addLogEntry(entry types.LogEntry) {
	e.mu.Lock()
	defer e.mu.Unlock()

	now := time.Now()
	e.logEntries.PushBack(entry)
	fmt.Printf("After PushBack, len: %d\n", e.logEntries.Len())

	// Insert to DB
	if err := e.storage.InsertLogEntry(entry); err != nil {
		log.Printf("Error inserting log entry to DB: %v", err)
	}

	// Add to latencies, but only for successful requests
	if entry.StatusCode < 400 && entry.Latency > 0 {
		e.latencies = append(e.latencies, float64(entry.Latency.Milliseconds()))
	}

	e.dirty = true

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

	for {
		select {
		case <-ticker.C:
			e.mu.Lock() // Lock to check and modify dirty flag
			if e.dirty {
				e.calculateMetrics()
				e.detectAnomalies()
				// Append to history
				if wm, ok := e.metrics.Windows["1m"]; ok {
					tp := types.TrendPoint{
						RPS:       wm.RPS,
						P95Latency: wm.P95Latency,
						ErrorRate: wm.ErrorRate,
					}
					e.metricsHistory = append(e.metricsHistory, tp)
					if len(e.metricsHistory) > maxMetricsHistory {
						e.metricsHistory = e.metricsHistory[1:]
					}
					e.rpsHistory = append(e.rpsHistory, wm.RPS)
					if len(e.rpsHistory) > maxMetricsHistory {
						e.rpsHistory = e.rpsHistory[1:]
					}
					e.errorRateHistory = append(e.errorRateHistory, wm.ErrorRate)
					if len(e.errorRateHistory) > maxMetricsHistory {
						e.errorRateHistory = e.errorRateHistory[1:]
					}
					e.latencyHistory = append(e.latencyHistory, float64(wm.P95Latency.Milliseconds()))
					if len(e.latencyHistory) > maxMetricsHistory {
						e.latencyHistory = e.latencyHistory[1:]
					}
				}
				e.metrics.TrendHistory = make([]types.TrendPoint, len(e.metricsHistory))
				copy(e.metrics.TrendHistory, e.metricsHistory)
				e.metricsChan <- e.metrics
				e.dirty = false
			}

			// Periodic prune
			if time.Since(e.lastPrune) > pruneInterval {
				now := time.Now()
				e.pruneDB(now)
				e.lastPrune = now
			}
			e.mu.Unlock() // Unlock after operations
		case <-e.doneChan:
			return
		default:
			// For live monitoring, send metrics if dirty
			if !e.initialScan {
				e.mu.Lock()
				if e.dirty {
					e.calculateMetrics()
					e.detectAnomalies()
					e.metricsChan <- e.metrics
					e.dirty = false
				}
				e.mu.Unlock()
			}
		}
	}
}

func (e *Engine) calculateMetrics() {
	e.metrics.Windows = make(map[string]types.WindowedMetrics)

	if e.initialScan {
		// For initial scan, compute metrics for all entries
		entries := []types.LogEntry{}
		for elem := e.logEntries.Front(); elem != nil; elem = elem.Next() {
			entries = append(entries, elem.Value.(types.LogEntry))
		}
		wm := e.computeWindowedMetrics(entries, 0)
		e.metrics.Windows["all"] = wm
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
}

func (e *Engine) computeWindowedMetrics(entries []types.LogEntry, window time.Duration) types.WindowedMetrics {
	if len(entries) == 0 {
		return types.WindowedMetrics{
			TopEndpoints:           make(map[string]int),
			StatusCodeDistribution: make(map[string]int),
			Custom:                 make(map[string]int),
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
	// Statistical anomaly detection using rolling averages and standard deviations
	wm, ok := e.metrics.Windows["1h"]
	if !ok {
		return
	}

	// Detect RPS anomalies
	if len(e.rpsHistory) > 10 {
		avgRPS, stdRPS := calculateMeanStd(e.rpsHistory)
		currentRPS := wm.RPS
		if currentRPS > avgRPS+3*stdRPS || currentRPS < avgRPS-3*stdRPS {
			e.metrics.Anomalies = append(e.metrics.Anomalies, types.Anomaly{
				Timestamp: time.Now(),
				Type:      "RPS Anomaly",
				Message:   fmt.Sprintf("RPS %.2f is outside 3-sigma range (avg: %.2f, std: %.2f)", currentRPS, avgRPS, stdRPS),
			})
		}
	}

	// Detect Error Rate anomalies
	if len(e.errorRateHistory) > 10 {
		avgErr, stdErr := calculateMeanStd(e.errorRateHistory)
		currentErr := wm.ErrorRate
		if currentErr > avgErr+3*stdErr || currentErr < avgErr-3*stdErr {
			e.metrics.Anomalies = append(e.metrics.Anomalies, types.Anomaly{
				Timestamp: time.Now(),
				Type:      "Error Rate Anomaly",
				Message:   fmt.Sprintf("Error rate %.2f%% is outside 3-sigma range (avg: %.2f%%, std: %.2f%%)", currentErr, avgErr, stdErr),
			})
		}
	}

	// Detect Latency anomalies
	if len(e.latencyHistory) > 10 {
		avgLat, stdLat := calculateMeanStd(e.latencyHistory)
		currentLat := float64(wm.P95Latency.Milliseconds())
		if currentLat > avgLat+3*stdLat || currentLat < avgLat-3*stdLat {
			e.metrics.Anomalies = append(e.metrics.Anomalies, types.Anomaly{
				Timestamp: time.Now(),
				Type:      "Latency Anomaly",
				Message:   fmt.Sprintf("P95 latency %v is outside 3-sigma range (avg: %.2fms, std: %.2fms)", wm.P95Latency, avgLat, stdLat),
			})
		}
	}

	// Baseline drift detection (simple: check if average is trending)
	if len(e.rpsHistory) > 20 {
		recentAvg := average(e.rpsHistory[len(e.rpsHistory)-10:])
		olderAvg := average(e.rpsHistory[len(e.rpsHistory)-20 : len(e.rpsHistory)-10])
		if recentAvg > olderAvg*1.2 || recentAvg < olderAvg*0.8 {
			e.metrics.Anomalies = append(e.metrics.Anomalies, types.Anomaly{
				Timestamp: time.Now(),
				Type:      "Baseline Drift",
				Message:   fmt.Sprintf("RPS baseline drift detected (recent avg: %.2f, older avg: %.2f)", recentAvg, olderAvg),
			})
		}
	}
}

func calculateMeanStd(data []float64) (float64, float64) {
	if len(data) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(len(data))
	sumSq := 0.0
	for _, v := range data {
		sumSq += (v - mean) * (v - mean)
	}
	std := 0.0
	if len(data) > 1 {
		std = sumSq / float64(len(data)-1)
		std = math.Sqrt(std)
	}
	return mean, std
}

func average(data []float64) float64 {
	if len(data) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	return sum / float64(len(data))
}

