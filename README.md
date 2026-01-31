# Pulsewatch

Pulsewatch is a real-time log analysis tool.
A fast and efficient log analysis tool that provides real-time insights, anomaly detection, and a live terminal dashboard.

## Commands

### `pulsewatch watch [file]`

Tails a log file and displays a live dashboard of metrics and anomalies. If no file is specified, it reads from stdin.

#### Flags:

*   `-i`, `--initial-scan`: Process existing logs before tailing for new ones. (default: `false`)

### `pulsewatch replay [file]`

Reads logs from a file and simulates real-time processing, displaying the dashboard as if it were live.

#### Flags:

*   `-s`, `--speed`: Speed multiplier for replaying logs. (default: `1.0`)
