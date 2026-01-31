# Pulsewatch

Pulsewatch is a real-time log analysis tool.
A fast and efficient log analysis tool that provides real-time insights, anomaly detection, and a live terminal dashboard.

## Installation

### Prerequisites
- Go 1.19 or later

### Build from Source
```bash
git clone https://github.com/nitis/pulseWatch.git
cd pulseWatch
go build ./cmd/pulsewatch
```

The binary `pulsewatch` will be created in the current directory.

## Features

*   **Real-time Log Analysis:** Process logs from files or stdin.
*   **Interactive TUI:** Live dashboard displaying key metrics.
*   **Log Filtering:** Interactively filter raw log lines within the TUI.
*   **Key Metrics:** Displays Request Per Second (RPS), Error Rate, Latency Percentiles (P50, P90, P95, P99).
*   **Top Endpoints:** Shows frequently accessed endpoints.
*   **Status Code Distribution:** Provides a breakdown of HTTP status codes (e.g., 2xx, 4xx, 5xx).
*   **Anomaly Detection:** Basic detection for high error rates or high latency.
*   **Time-Based Metrics:** Configurable time windows (1 minute, 5 minutes, 1 hour) for metrics calculation.
*   **Local Storage:** Persistent SQLite database for logs, survives restarts.
*   **Trend Visualization:** ASCII-based charts showing RPS, latency, and error rate trends over time.
*   **Custom Metric Definitions:** User-defined metrics based on regex matching or field extraction from log entries.
*   **Advanced Anomaly Detection:** Statistical anomaly detection using rolling averages, standard deviations, and baseline drift detection.

## Commands

### `pulsewatch watch [file]`

The `watch` command provides real-time log analysis capabilities.

#### Modes of Operation:

1.  **Historical Analysis (One-shot report):**
    *   **Usage:** `pulsewatch watch --initial-scan [file]`
    *   **Description:** Reads the *entire* specified log file once, processes all its contents, displays a comprehensive report in the interactive TUI, and then exits. This mode is ideal for quick inspection of existing log files.
    *   **Usage:** `pulsewatch watch --initial-scan [file]`
    *   **Description:** Reads the entire log file, processes all entries, and displays a comprehensive historical report.
    *   **Flags:**
        *   `-i`, `--initial-scan`: Activates this mode.
        *   `-c`, `--config`: Config file (YAML) for custom metrics (optional).
2.  **Live Tailing (Continuous monitoring):**
    *   **Usage:** `pulsewatch watch [file]`
    *   **Description:** Tails the log file in real-time, displaying a live dashboard with metrics, trends, and anomalies.
    *   **Flags:**
        *   `-c`, `--config`: Config file (YAML) for custom metrics (optional).

### `pulsewatch replay [file]`

Reads logs from a file and simulates real-time processing, displaying the dashboard as if it were live.

#### Flags:

*   `-s`, `--speed`: Speed multiplier for replaying logs. (default: `1.0`)

## Examples

### Basic Live Monitoring
```bash
./pulsewatch watch access.log
```
Tails `access.log` in real-time, showing live metrics and trends.

### Historical Analysis
```bash
./pulsewatch watch --initial-scan nginx.log
```
Processes the entire `nginx.log` file and displays a comprehensive report in the TUI.

### With Custom Metrics
```bash
./pulsewatch watch --config metrics.yaml access.log
```
Monitors `access.log` with custom metrics defined in `metrics.yaml`.

### Replay Mode
```bash
./pulsewatch replay access.log --speed 2.0
```
Replays `access.log` at 2x speed for testing or demonstration.

### TUI Controls
- **q** or **Ctrl+C**: Quit the application.
- **esc**: Clear the log filter.
- **enter**: Apply the current filter.
- **Filter Input**: Type to filter displayed logs in real-time.

## Configuration

### Custom Metrics Configuration

Define custom metrics in a YAML config file to count specific patterns or extract values:

```yaml
custom_metrics:
  - name: "error_logs"
    type: "count"
    filter: "regex:ERROR"
  - name: "api_calls"
    type: "count"
    filter: "regex:GET /api"
  - name: "hits_by_ip_127"
    type: "count"
    filter: "regex:127\\.0\\.0\\.1"
  - name: "post_requests"
    type: "count"
    filter: "regex:POST "
```

Use with: `pulsewatch watch --config config.yaml [file]`

Supported filter types:
- `regex:<pattern>`: Matches log message against regex pattern.

### Database Configuration

PulseWatch uses SQLite for persistence. The database file `pulsewatch.db` is created automatically in the current directory. It stores parsed log entries for historical analysis and survives application restarts.

### Window Sizes

Metrics are calculated over configurable time windows:
- 1 minute (1m)
- 5 minutes (5m)
- 1 hour (1h)

For historical scans (`--initial-scan`), a special "all" window covers the entire file.

### Grouping and Aggregation Examples

Use custom metrics to group hits:
- By IP: `filter: "regex:<IP>"`
- By API endpoint: `filter: "regex:GET /api/<endpoint>"`
- By time (e.g., errors in last hour): Use regex on timestamp if present.
- By status code: `filter: "regex: 500 "`

### Log Format Support

PulseWatch automatically detects and parses multiple log formats:
- **JSON Logs:** Parsed using key-value extraction from JSON objects.
- **Nginx Logs:** Standard combined access log format.
- **Apache Logs:** Common access log format.
- **Custom Logs:** Falls back to line-based parsing for unrecognized formats.

Demo log files included: `nginx.log`, `apache.log`, `json.log`.

### Troubleshooting

- **No metrics displayed:** Ensure the log file exists and contains parseable entries. Check for supported formats.
- **High CPU usage:** Large log files in live mode may cause performance issues; use `--initial-scan` for static analysis.
- **Database errors:** Ensure write permissions in the current directory for `pulsewatch.db`.

### Contributing

Contributions are welcome! Please submit issues or pull requests on GitHub.