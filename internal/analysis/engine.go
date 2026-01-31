package analysis

import (
	"container/list"
	"sync"
	"time"

	"github.com/VividCortex/ewma"
	"github.com/montanaflynn/stats"
	"github.com/nitis/pulseWatch/internal/types"
)

const (
	defaultWindow         = 5 * time.Minute
	defaultTickInterval   = 1 * time.Second
	latencyPercentile     = 95
	errorRateSpikeThreshold = 3.0 // 3x increase
)

// Engine is the analysis engine for pulsewatch.
type Engine struct {
	windowDuration time.Duration
	tickInterval   time.Duration

	logEntries *list.List
	latencies  []float64
	mu         sync.Mutex
	dirty      bool // New field to track if new logs have been added

	rpsEWMA ewma.MovingAverage

	metrics                types.Metrics
	metricsChan            chan types.Metrics
	doneChan               chan struct{}
	statusCodeDistribution map[string]int
}

// NewEngine creates a new analysis engine.
func NewEngine() *Engine {
	return &Engine{
		windowDuration: defaultWindow,
		tickInterval:   defaultTickInterval,
		logEntries:     list.New(),
		rpsEWMA:        ewma.NewMovingAverage(),
		metricsChan:    make(chan types.Metrics),
		doneChan:       make(chan struct{}),
		metrics: types.Metrics{
			TopEndpoints:           make(map[string]int),
			Anomalies:              []types.Anomaly{},
			StartTime:              time.Now(),
			StatusCodeDistribution: make(map[string]int),
		},
		statusCodeDistribution: make(map[string]int),
		dirty:                  false, // Initialize dirty flag
	}
}

// Start begins the analysis engine's processing loop.
func (e *Engine) Start(logChan <-chan types.LogEntry) <-chan types.Metrics {
	go e.processLogs(logChan)
	go e.runTicker()
	return e.metricsChan
}

// Stop halts the analysis engine.
func (e *Engine) Stop() {
	close(e.doneChan)
}

func (e *Engine) processLogs(logChan <-chan types.LogEntry) {
	for {
		select {
		case logEntry, ok := <-logChan:
			if !ok {
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

	// Add to latencies, but only for successful requests
	if entry.StatusCode < 400 && entry.Latency > 0 {
		e.latencies = append(e.latencies, float64(entry.Latency.Milliseconds()))
	}

	if entry.Endpoint != "" {
		e.metrics.TopEndpoints[entry.Endpoint]++
	}

	e.metrics.TotalRequests++
	if entry.StatusCode >= 400 {
		e.metrics.TotalErrors++
	}

	// Update status code distribution
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
	e.statusCodeDistribution[statusCodeCategory]++

	e.dirty = true // Mark as dirty when a new log is added

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
				e.metricsChan <- e.metrics
				e.dirty = false // Reset dirty flag after sending
			}
			e.mu.Unlock() // Unlock after operations
		case <-e.doneChan:
			return
		}
	}
}

func (e *Engine) calculateMetrics() {
	e.mu.Lock()
	defer e.mu.Unlock()

	duration := time.Since(e.metrics.StartTime).Seconds()
	if duration == 0 {
		return
	}

	e.metrics.RPS = float64(e.metrics.TotalRequests) / duration

	if e.metrics.TotalRequests > 0 {
		e.metrics.ErrorRate = (float64(e.metrics.TotalErrors) / float64(e.metrics.TotalRequests)) * 100
	}

	if len(e.latencies) > 0 {
		p50, _ := stats.Percentile(e.latencies, 50)
		p90, _ := stats.Percentile(e.latencies, 90)
		p95, _ := stats.Percentile(e.latencies, 95)
		p99, _ := stats.Percentile(e.latencies, 99)
		e.metrics.P50Latency = time.Duration(p50) * time.Millisecond
		e.metrics.P90Latency = time.Duration(p90) * time.Millisecond
		e.metrics.P95Latency = time.Duration(p95) * time.Millisecond
		e.metrics.P99Latency = time.Duration(p99) * time.Millisecond
	}

	// Update the metrics with the current status code distribution
	e.metrics.StatusCodeDistribution = make(map[string]int)
	for category, count := range e.statusCodeDistribution {
		e.metrics.StatusCodeDistribution[category] = count
	}
}

func (e *Engine) detectAnomalies() {
	// Simple anomaly detection for now
	// In a real system, this would be more sophisticated

	// Error Rate Spike
	// This is a placeholder. A real implementation would compare against a baseline.
	if e.metrics.ErrorRate > 10.0 && len(e.metrics.Anomalies) == 0 { // Add anomaly only once for now
		e.metrics.Anomalies = append(e.metrics.Anomalies, types.Anomaly{
			Timestamp: time.Now(),
			Type:      "High Error Rate",
			Message:   "Error rate is above 10%",
		})
	}

	// Latency Jump
	if e.metrics.P95Latency > 1*time.Second && len(e.metrics.Anomalies) <= 1 { // Add anomaly only once for now
		e.metrics.Anomalies = append(e.metrics.Anomalies, types.Anomaly{
			Timestamp: time.Now(),
			Type:      "High Latency",
			Message:   "P95 latency is over 1 second",
		})
	}
}

