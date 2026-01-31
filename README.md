# Pulsewatch

Pulsewatch is a real-time log analysis tool.
A fast and efficient log analysis tool that provides real-time insights, anomaly detection, and a live terminal dashboard.

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

## Custom Metrics Configuration

Define custom metrics in a YAML config file:

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
```

Use with: `pulsewatch watch --config config.yaml [file]`

Supported filter types:
- `regex:<pattern>`: Matches log message against regex pattern.

### Grouping and Aggregation Examples

Use custom metrics to group hits:
- By IP: `filter: "regex:<IP>"`
- By API endpoint: `filter: "regex:GET /api/<endpoint>"`
- By time (e.g., errors in last hour): Use regex on timestamp if present.
- By status code: `filter: "regex: 500 "`

### Log Format Support

pulseWatch supports multiple log formats automatically:
- **JSON Logs:** Parsed using key-value extraction.
- **Nginx Logs:** Standard access log format.
- **Apache Logs:** Common access log format.
- **Custom Logs:** Falls back to line-based parsing.

Demo log files included: `nginx.log`, `apache.log`, `json.log`.