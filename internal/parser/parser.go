package parser

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mssola/user_agent"
	"github.com/nitis/pulseWatch/internal/types"
)

// Parser is the interface for parsing log lines.
type Parser interface {
	Parse(line string) (types.LogEntry, bool)
}

// MultiParser tries a series of parsers and returns the result of the first one that succeeds.
type MultiParser struct {
	parsers []Parser
}

// NewMultiParser creates a new MultiParser.
func NewMultiParser(parsers ...Parser) *MultiParser {
	return &MultiParser{parsers: parsers}
}

// Parse runs the log line through the configured parsers.
func (p *MultiParser) Parse(line string) (types.LogEntry, bool) {
	for _, parser := range p.parsers {
		fmt.Printf("MultiParser: trying parser %T\n", parser)
		if entry, ok := parser.Parse(line); ok {
			fmt.Printf("MultiParser: parser %T returned true\n", parser)
			return entry, true
		}
	}
	fmt.Println("MultiParser: no parser returned true")
	return types.LogEntry{}, false
}

// JSONParser parses JSON log lines.
type JSONParser struct{}

// Parse attempts to parse a line as JSON.
func (p *JSONParser) Parse(line string) (types.LogEntry, bool) {
	fmt.Println("JSONParser: Parse called")
	var entry types.LogEntry
	var raw map[string]interface{}

	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		fmt.Println("JSONParser: unmarshal failed")
		return types.LogEntry{}, false
	}
	fmt.Println("JSONParser: unmarshal success")

	entry.Fields = make(map[string]interface{})

	// Look for common timestamp fields
	if ts, ok := raw["timestamp"]; ok {
		entry.Timestamp = parseTimestamp(ts)
	} else if ts, ok := raw["ts"]; ok {
		entry.Timestamp = parseTimestamp(ts)
	} else if ts, ok := raw["time"]; ok {
		entry.Timestamp = parseTimestamp(ts)
	} else {
		entry.Timestamp = time.Now()
	}

	// Look for common message fields
	if msg, ok := raw["message"]; ok {
		if s, ok := msg.(string); ok {
			entry.Message = s
		}
	} else if msg, ok := raw["msg"]; ok {
		if s, ok := msg.(string); ok {
			entry.Message = s
		}
	}

	// Look for common level fields
	if level, ok := raw["level"]; ok {
		if s, ok := level.(string); ok {
			entry.Level = parseLevel(s)
		}
	} else {
		entry.Level = types.InfoLevel
	}

	// Look for common status code fields
	if status, ok := raw["status"]; ok {
		if s, ok := status.(float64); ok {
			entry.StatusCode = int(s)
		}
	} else if code, ok := raw["code"]; ok {
		if s, ok := code.(float64); ok {
			entry.StatusCode = int(s)
		}
	}

	// Look for common latency fields
	if latency, ok := raw["latency"]; ok {
		if l, ok := latency.(float64); ok {
			entry.Latency = time.Duration(l) * time.Millisecond
		}
	}

	// Look for common endpoint fields
	if endpoint, ok := raw["endpoint"]; ok {
		if e, ok := endpoint.(string); ok {
			entry.Endpoint = e
		}
	} else if path, ok := raw["path"]; ok {
		if p, ok := path.(string); ok {
			entry.Endpoint = p
		}
	}

	// Add all raw fields to the entry's Fields map
	for k, v := range raw {
		entry.Fields[k] = v
	}

	return entry, true
}

// NginxParser parses Nginx access log lines.
type NginxParser struct {
	regex *regexp.Regexp
}

// NewNginxParser creates a new NginxParser.
func NewNginxParser() *NginxParser {
	// A common Nginx log format regex
	re := regexp.MustCompile(`(?P<remote_addr>\S+) - (?P<remote_user>\S+) \[(?P<time_local>.+)\] "(?P<request>\S+ \S+ \S+)" (?P<status>\d{3}) (?P<body_bytes_sent>\d+) "(?P<http_referer>[^"]*)" "(?P<http_user_agent>[^"]*)" (?P<request_time>\S+)`)
	return &NginxParser{regex: re}
}

// Parse attempts to parse a line as an Nginx access log.
func (p *NginxParser) Parse(line string) (types.LogEntry, bool) {
	fmt.Println("NginxParser: Parse called")
	// Temporarily disable NginxParser
	return types.LogEntry{}, false
	match := p.regex.FindStringSubmatch(line)
	if match == nil {
		return types.LogEntry{}, false
	}

	result := make(map[string]string)
	for i, name := range p.regex.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}
	
	ts, err := time.Parse("02/Jan/2006:15:04:05 -0700", result["time_local"])
	if err != nil {
		ts = time.Now()
	}

	status, _ := strconv.Atoi(result["status"])

	requestParts := strings.Split(result["request"], " ")
	var endpoint string
	if len(requestParts) > 1 {
		endpoint = requestParts[1]
	}

	latency := 0.0
	if rt, err := strconv.ParseFloat(result["request_time"], 64); err == nil {
		latency = rt
	}

	ua := user_agent.New(result["http_user_agent"])
	browserName, browserVersion := ua.Browser()

	entry := types.LogEntry{
		Timestamp:  ts,
		Message:    line,
		StatusCode: status,
		Endpoint:   endpoint,
		Latency:    time.Duration(latency * float64(time.Second)),
		Fields: map[string]interface{}{
			"remote_addr":      result["remote_addr"],
			"request":          result["request"],
			"http_referer":     result["http_referer"],
			"user_agent":       result["http_user_agent"],
			"browser_name":     browserName,
			"browser_version":  browserVersion,
			"is_mobile":        ua.Mobile(),
		},
	}

	if status >= 400 {
		entry.Level = types.ErrorLevel
	} else {
		entry.Level = types.InfoLevel
	}


	return entry, true
}


// LineParser is a fallback parser that treats the whole line as a message.
type LineParser struct{}

// Parse treats the entire line as a message.
func (p *LineParser) Parse(line string) (types.LogEntry, bool) {
	fmt.Println("LineParser: Parse called")
	level := types.InfoLevel
	if strings.Contains(strings.ToLower(line), "error") {
		level = types.ErrorLevel
	} else if strings.Contains(strings.ToLower(line), "warn") {
		level = types.WarnLevel
	}

	return types.LogEntry{
		Timestamp: time.Now(),
		Message:   line,
		Level:     level,
	}, true
}

func parseTimestamp(ts interface{}) time.Time {
	switch v := ts.(type) {
	case string:
		// Attempt to parse a few common formats
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
			return t
		}
		// Unix timestamp in string
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(i, 0)
		}

	case float64:
		// Unix timestamp
		return time.Unix(int64(v), 0)
	}
	return time.Now()
}

func parseLevel(level string) types.LogLevel {
	l := strings.ToUpper(level)
	switch l {
	case "INFO":
		return types.InfoLevel
	case "WARN", "WARNING":
		return types.WarnLevel
	case "ERROR", "ERR":
		return types.ErrorLevel
	case "DEBUG":
		return types.DebugLevel
	default:
		return types.UnknownLevel
	}
}