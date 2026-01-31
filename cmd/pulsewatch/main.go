package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/nitis/pulseWatch/internal/analysis"
	"github.com/nitis/pulseWatch/internal/ingest"
	"github.com/nitis/pulseWatch/internal/parser"
	"github.com/nitis/pulseWatch/internal/replay"
	"github.com/nitis/pulseWatch/internal/tui"
	"github.com/nitis/pulseWatch/internal/types"
	"github.com/spf13/cobra"
	"github.com/charmbracelet/bubbletea"
)

func printReport(metrics types.Metrics) {
	if wm, ok := metrics.Windows["all"]; ok {
		fmt.Println("Historical Report")
		fmt.Println()

		fmt.Printf("Total Requests: %d | Errors: %.2f%%\n", wm.TotalRequests, wm.ErrorRate)
		fmt.Println()

		fmt.Printf("P50: %v | P90: %v | P95: %v | P99: %v\n", wm.P50Latency.Truncate(time.Millisecond), wm.P90Latency.Truncate(time.Millisecond), wm.P95Latency.Truncate(time.Millisecond), wm.P99Latency.Truncate(time.Millisecond))
		fmt.Println()

		if len(wm.TopEndpoints) > 0 {
			fmt.Println("Top Endpoints:")
			type endpointCount struct {
				endpoint string
				count    int
			}
			var ec []endpointCount
			for ep, cnt := range wm.TopEndpoints {
				ec = append(ec, endpointCount{ep, cnt})
			}
			sort.Slice(ec, func(i, j int) bool { return ec[i].count > ec[j].count })
			for i, e := range ec {
				if i >= 5 {
					break
				}
				fmt.Printf("%s: %d\n", e.endpoint, e.count)
			}
			fmt.Println()
		}

		fmt.Println("Status Codes:")
		for code, count := range wm.StatusCodeDistribution {
			fmt.Printf("%s: %d\n", code, count)
		}
		fmt.Println()

		if len(wm.Custom) > 0 {
			fmt.Println("Custom Metrics:")
			for name, value := range wm.Custom {
				fmt.Printf("%s: %d\n", name, value)
			}
			fmt.Println()
		}
	}
}

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
		for line := range rawLogChan {
			select {
			case rawLogChanForParser <- line:
			case <-ctx.Done():
				return
			}
			select {
			case rawLogChanForTUI <- line:
			case <-ctx.Done():
				return
			}
		}
	}()

	multiParser := parser.NewMultiParser(
		&parser.JSONParser{},
		parser.NewNginxParser(),
		&parser.LineParser{},
	)

	logEntryChan := make(chan types.LogEntry)
	go func() {
		defer close(logEntryChan)
		for line := range rawLogChanForParser {
			if entry, ok := multiParser.Parse(line); ok {
				logEntryChan <- entry
			}
		}
	}()

	initialScan, _ := cmd.Flags().GetBool("initial-scan")
	engine, err := analysis.NewEngine("pulsewatch.db", initialScan, []types.CustomMetric{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating engine: %v\n", err)
		os.Exit(1)
	}
	metricsChan := engine.Start(logEntryChan)

	if initialScan {
		// For initial scan, wait for metrics and print report
		metrics := <-metricsChan
		printReport(metrics)
	} else {
		model := tui.NewModel(metricsChan, rawLogChanForTUI, initialScan)
		p := tea.NewProgram(model, tea.WithAltScreen())

		if err := p.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting TUI: %v\n", err)
			os.Exit(1)
		}
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
		for line := range rawLogChan {
			select {
			case rawLogChanForParser <- line:
			case <-ctx.Done():
				return
			}
			select {
			case rawLogChanForTUI <- line:
			case <-ctx.Done():
				return
			}
		}
	}()

	multiParser := parser.NewMultiParser(
		&parser.JSONParser{},
		parser.NewNginxParser(),
		&parser.LineParser{},
	)

	logEntryChan := make(chan types.LogEntry)
	go func() {
		defer close(logEntryChan)
		for line := range rawLogChanForParser {
			if entry, ok := multiParser.Parse(line); ok {
				logEntryChan <- entry
			}
		}
	}()

	initialScan, _ := cmd.Flags().GetBool("initial-scan")
	engine, err := analysis.NewEngine("pulsewatch.db", initialScan, []types.CustomMetric{})
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