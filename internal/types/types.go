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

// Metrics holds the aggregated data points for the TUI display.
type Metrics struct {
	RPS         float64
	ErrorRate   float64
	P50Latency  time.Duration
	P90Latency  time.Duration
	P95Latency  time.Duration
	P99Latency  time.Duration
	Anomalies   []Anomaly
	TopEndpoints map[string]int
	TotalRequests int
	TotalErrors   int
	StartTime   time.Time
}