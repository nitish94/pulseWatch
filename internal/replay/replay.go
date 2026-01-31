package replay

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"time"
)

// Replayer reads a log file and sends entries to a channel at a specified speed.
type Replayer struct {
	filePath string
	speed    float64
}

// NewReplayer creates a new Replayer.
func NewReplayer(filePath string, speed float64) *Replayer {
	return &Replayer{
		filePath: filePath,
		speed:    speed,
	}
}

// Replay reads the log file and sends log entries to the output channel.
func (r *Replayer) Replay(ctx context.Context) (<-chan string, error) {
	file, err := os.Open(r.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	outChan := make(chan string)
	scanner := bufio.NewScanner(file)

	go func() {
		defer file.Close()
		defer close(outChan)

		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}

		if err := scanner.Err(); err != nil {
			fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
			return
		}

		delay := time.Duration(1000/r.speed) * time.Millisecond

		for _, line := range lines {
			select {
			case <-ctx.Done():
				return
			case outChan <- line:
				time.Sleep(delay)
			}
		}
	}()

	return outChan, nil
}
