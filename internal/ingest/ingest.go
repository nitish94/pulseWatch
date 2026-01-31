package ingest

import (
	"bufio"
	"context"
	"io"
	"os"

	"github.com/hpcloud/tail"
)

// Ingester is the interface for log ingestion.
type Ingester interface {
	Ingest(ctx context.Context) (<-chan string, error)
}

// FileIngester tails a log file.
type FileIngester struct {
	FilePath    string
	InitialScan bool
}

// NewFileIngester creates a new FileIngester.
func NewFileIngester(filePath string, initialScan bool) *FileIngester {
	return &FileIngester{FilePath: filePath, InitialScan: initialScan}
}

// Ingest starts tailing the file and returns a channel of log lines.
func (i *FileIngester) Ingest(ctx context.Context) (<-chan string, error) {
	lines := make(chan string, 1000) // Buffered channel to prevent deadlock during initial scan

	// Phase 1: Initial scan if requested
	if i.InitialScan {
		file, err := os.Open(i.FilePath)
		if err != nil {
			return nil, err
		}
		defer file.Close() // Close the file when the function exits

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}

	// Phase 2: Tail for new lines
	// Always start tailing from the current end of the file for new updates.
	t, err := tail.TailFile(i.FilePath, tail.Config{
		Follow: true,
		ReOpen: true, // Re-open the file if it's rotated
		Location: &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}, // Start from the end for new lines
	})
	if err != nil {
		return nil, err
	}

	go func() {
		defer close(lines)
		for {
			select {
			case line := <-t.Lines:
				if line != nil {
					lines <- line.Text
				}
			case <-ctx.Done():
				t.Stop()
				return
			}
		}
	}()

	return lines, nil
}

// StdinIngester reads from standard input.
type StdinIngester struct{}

// NewStdinIngester creates a new StdinIngester.
func NewStdinIngester() *StdinIngester {
	return &StdinIngester{}
}

// Ingest starts reading from stdin and returns a channel of log lines.
func (i *StdinIngester) Ingest(ctx context.Context) (<-chan string, error) {
	lines := make(chan string)
	scanner := bufio.NewScanner(os.Stdin)

	go func() {
		defer close(lines)
		for scanner.Scan() {
			select {
			case lines <- scanner.Text():
			case <-ctx.Done():
				return
			}
		}
	}()

	return lines, nil
}