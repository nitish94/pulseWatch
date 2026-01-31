package main

import (
	"context"
	"fmt"
	"log" // Added log import
	"os"
	"os/signal"
	"syscall"

	"github.com/nitis/pulseWatch/internal/analysis"
	"github.com/nitis/pulseWatch/internal/ingest"
	"github.com/nitis/pulseWatch/internal/parser"
	"github.com/nitis/pulseWatch/internal/replay"
	"github.com/nitis/pulseWatch/internal/tui"
	"github.com/nitis/pulseWatch/internal/types"
	"github.com/spf13/cobra"
	"github.com/charmbracelet/bubbletea"
)

var rootCmd = &cobra.Command{
	Use:   "pulsewatch",
	Short: "Pulsewatch is a real-time log analysis tool.",
	Long:  `A fast and efficient log analysis tool that provides real-time insights, anomaly detection, and a live terminal dashboard.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var watchCmd = &cobra.Command{
	Use:   "watch [file]",
	Short: "Watch a log file in real-time",
	Long:  `Tails a log file and displays a live dashboard of metrics and anomalies. If no file is specified, it reads from stdin.`,
	Args:  cobra.MaximumNArgs(1),
	Run:   runWatch,
}

var replayCmd = &cobra.Command{
	Use:   "replay [file]",
	Short: "Replay logs from a file",
	Long:  `Reads logs from a file and simulates real-time processing, displaying the dashboard as if it were live.`,
	Args:  cobra.ExactArgs(1),
	Run:   runReplay,
}

func init() {
	replayCmd.Flags().Float64P("speed", "s", 1.0, "Speed multiplier for replaying logs")
	watchCmd.Flags().BoolP("initial-scan", "i", false, "Process existing logs before tailing for new ones")
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(replayCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Whoops. There was an error while executing your command '%s'", err)
		os.Exit(1)
	}
}

func runWatch(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	var ingester ingest.Ingester
	if len(args) > 0 {
		initialScan, _ := cmd.Flags().GetBool("initial-scan")
		ingester = ingest.NewFileIngester(args[0], initialScan)
	} else {
		fmt.Println("Watching stdin. Press Ctrl+C to exit.")
		ingester = ingest.NewStdinIngester()
	}

	rawLogChan, err := ingester.Ingest(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting ingestion: %v\n", err)
		os.Exit(1)
	}

	// Fan-out rawLogChan to separate channels for parser and TUI
	rawLogChanForParser := make(chan string)
	rawLogChanForTUI := make(chan string)

	go func() {
		defer close(rawLogChanForParser)
		defer close(rawLogChanForTUI)
		log.Println("Fan-out: Starting goroutine")
		for line := range rawLogChan {
			log.Println("Fan-out: Received line from rawLogChan:", line)
			select {
			case rawLogChanForParser <- line:
				log.Println("Fan-out: Sent line to parser chan")
			case <-ctx.Done():
				log.Println("Fan-out: Context cancelled during send to parser")
				return
			}
			select {
			case rawLogChanForTUI <- line:
				log.Println("Fan-out: Sent line to TUI chan")
			case <-ctx.Done():
				log.Println("Fan-out: Context cancelled during send to TUI")
				return
			}
		}
		log.Println("Fan-out: rawLogChan closed, fan-out goroutine exiting")
	}()

	multiParser := parser.NewMultiParser(
		&parser.JSONParser{},
		parser.NewNginxParser(),
		&parser.LineParser{},
	)

	logEntryChan := make(chan types.LogEntry)
	go func() {
		defer close(logEntryChan)
		log.Println("Parser: Starting goroutine")
		for line := range rawLogChanForParser { // Now reads from rawLogChanForParser
			log.Println("Parser: Received line from rawLogChanForParser:", line)
			if entry, ok := multiParser.Parse(line); ok {
				logEntryChan <- entry
				log.Println("Parser: Sent entry to logEntryChan")
			}
		}
		log.Println("Parser: rawLogChanForParser closed, parser goroutine exiting")
	}()

	initialScan, _ := cmd.Flags().GetBool("initial-scan")
	engine, err := analysis.NewEngine("pulsewatch.db", initialScan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating engine: %v\n", err)
		os.Exit(1)
	}
	metricsChan := engine.Start(logEntryChan)

	model := tui.NewModel(metricsChan, rawLogChanForTUI, initialScan) // TUI now reads from rawLogChanForTUI
	p := tea.NewProgram(model)

	if err := p.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting TUI: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Pulsewatch shutting down.")
}

func runReplay(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		cancel()
	}()

	speed, _ := cmd.Flags().GetFloat64("speed")
	replayer := replay.NewReplayer(args[0], speed)

	rawLogChan, err := replayer.Replay(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error starting replay: %v\n", err)
		os.Exit(1)
	}

	// Fan-out rawLogChan to separate channels for parser and TUI
	rawLogChanForParser := make(chan string)
	rawLogChanForTUI := make(chan string)

	go func() {
		defer close(rawLogChanForParser)
		defer close(rawLogChanForTUI)
		log.Println("Fan-out: Starting goroutine (Replay)")
		for line := range rawLogChan {
			log.Println("Fan-out: Received line from rawLogChan (Replay):", line)
			select {
			case rawLogChanForParser <- line:
				log.Println("Fan-out: Sent line to parser chan (Replay)")
			case <-ctx.Done():
				log.Println("Fan-out: Context cancelled during send to parser (Replay)")
				return
			}
			select {
			case rawLogChanForTUI <- line:
				log.Println("Fan-out: Sent line to TUI chan (Replay)")
			case <-ctx.Done():
				log.Println("Fan-out: Context cancelled during send to TUI (Replay)")
				return
			}
		}
		log.Println("Fan-out: rawLogChan closed, fan-out goroutine exiting (Replay)")
	}()

	multiParser := parser.NewMultiParser(
		&parser.JSONParser{},
		parser.NewNginxParser(),
		&parser.LineParser{},
	)

	logEntryChan := make(chan types.LogEntry)
	go func() {
		defer close(logEntryChan)
		log.Println("Parser: Starting goroutine (Replay)")
		for line := range rawLogChanForParser { // Now reads from rawLogChanForParser
			log.Println("Parser: Received line from rawLogChanForParser (Replay):", line)
			if entry, ok := multiParser.Parse(line); ok {
				logEntryChan <- entry
				log.Println("Parser: Sent entry to logEntryChan (Replay)")
			}
		}
		log.Println("Parser: rawLogChanForParser closed, parser goroutine exiting (Replay)")
	}()

	initialScan, _ := cmd.Flags().GetBool("initial-scan")
	engine, err := analysis.NewEngine("pulsewatch.db", initialScan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating engine: %v\n", err)
		os.Exit(1)
	}
	metricsChan := engine.Start(logEntryChan)

	model := tui.NewModel(metricsChan, rawLogChanForTUI, false) // TUI now reads from rawLogChanForTUI
	p := tea.NewProgram(model, tea.WithAltScreen())

	if err := p.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting TUI: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Pulsewatch shutting down.")
}