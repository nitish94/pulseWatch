package ingest

import (
	"bufio"
	"context"
	"fmt"
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
	lines := make(chan string, 1000)

	// One-shot read (if initialScan is true)
	if i.InitialScan {
		file, err := os.Open(i.FilePath)
		if err != nil {
			close(lines) // Ensure channel is closed on error
			return nil, err
		}
		// Goroutine to read the file and close the channel
		go func() {
			defer file.Close()
			defer close(lines)

			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				select {
				case lines <- scanner.Text():
				case <-ctx.Done():
					return
				}
			}
			if err := scanner.Err(); err != nil {
				fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
			}
		}()
		return lines, nil // Return here after setting up one-shot read
	}

	// Dynamic Tailing (if initialScan is false, i.e., default behavior)
	t, err := tail.TailFile(i.FilePath, tail.Config{
		Follow: true,
		ReOpen: true,
		Location: &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd}, // Always start from end for actual tailing
	})
	if err != nil {
		close(lines) // Ensure channel is closed on error
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