package types

import (
	"time"
)

// LogLevel defines the level of a log entry.
type LogLevel string

const (
	InfoLevel  LogLevel = "INFO"
	WarnLevel  LogLevel = "WARN"
	ErrorLevel LogLevel = "ERROR"
	DebugLevel LogLevel = "DEBUG"
	UnknownLevel LogLevel = "UNKNOWN"
)

// LogEntry represents a single, parsed log line.
type LogEntry struct {
	Timestamp time.Time
	Message   string
	Level     LogLevel
	StatusCode int
	Latency   time.Duration
	Endpoint  string
	Fields    map[string]interface{}
}

// Anomaly represents a detected anomaly in the log stream.
type Anomaly struct {
	Timestamp time.Time
	Type      string
	Message   string
}

// TrendPoint holds key metrics for trend visualization.
type TrendPoint struct {
	RPS       float64
	P95Latency time.Duration
	ErrorRate float64
}

// CustomMetric defines a user-defined metric.
type CustomMetric struct {
	Name   string
	Type   string
	Filter string
}

// WindowedMetrics holds metrics for a specific time window.
type WindowedMetrics struct {
	RPS         float64
	ErrorRate   float64
	P50Latency  time.Duration
	P90Latency  time.Duration
	P95Latency  time.Duration
	P99Latency  time.Duration
	TopEndpoints map[string]int
	TotalRequests int
	TotalErrors   int
	StatusCodeDistribution map[string]int
	Custom      map[string]int
}

// Metrics holds the aggregated data points for the TUI display.
type Metrics struct {
	Windows      map[string]WindowedMetrics // Key: "1m", "5m", "1h"
	Anomalies    []Anomaly
	StartTime    time.Time
	TrendHistory []TrendPoint // For trend visualization
}