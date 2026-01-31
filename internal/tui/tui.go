package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nitis/pulseWatch/internal/types"
)

const maxLogEntries = 1000

// TUI is the terminal user interface for pulsewatch.
type Model struct {
	metrics       types.Metrics
	spinner       spinner.Model
	width         int
	height        int
	metricsCh     <-chan types.Metrics
	rawLogsCh     <-chan string        // Changed from types.LogEntry
	logs          []string             // Changed from types.LogEntry
	filteredLogs  []string             // Changed from types.LogEntry
	logScrollPane viewport.Model
	filterInput   textinput.Model
	currentFilter string
}

type metricsMsg struct{ metrics types.Metrics }
type rawLogMsg struct{ line string } // Changed from types.LogEntry

// NewModel creates a new TUI model.
func NewModel(metricsCh <-chan types.Metrics, rawLogsCh <-chan string) Model { // Changed signature
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	ti := textinput.New()
	ti.Placeholder = "Filter logs..."
	ti.CharLimit = 256
	ti.Width = 20
	ti.Prompt = "Filter: "

	vp := viewport.New(0, 0)
	vp.SetContent("Waiting for logs...")
	vp.MouseWheelEnabled = true

	return Model{
		spinner:       s,
		metricsCh:     metricsCh,
		rawLogsCh:     rawLogsCh,
		logs:          []string{},
		filteredLogs:  []string{},
		filterInput:   ti,
		logScrollPane: vp,
	}
}

// Init initializes the TUI model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		m.filterInput.SetCursorMode(textinput.CursorBlink),
		m.filterInput.Focus(),
		m.waitForMetrics,
		m.waitForRawLogs,
	)
}

func (m Model) waitForMetrics() tea.Msg {
	return metricsMsg{<-m.metricsCh}
}

// New function to receive raw log entries
func (m Model) waitForRawLogs() tea.Msg {
	return rawLogMsg{<-m.rawLogsCh}
}

// Update handles updates to the TUI model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc": // Clear filter when esc is pressed
			if m.filterInput.Focused() {
				m.filterInput.Blur()
				m.filterInput.SetValue("")
				m.currentFilter = ""
				m.applyFilter()
			}
		case "enter": // Apply filter when enter is pressed
			if m.filterInput.Focused() {
				m.filterInput.Blur()
				m.currentFilter = m.filterInput.Value()
				m.applyFilter()
			}
		case "/": // Focus filter input on '/'
			m.filterInput.Focus()
		default:
			// If filter input is focused, send key messages to it
			if m.filterInput.Focused() {
				m.filterInput, cmd = m.filterInput.Update(msg)
				cmds = append(cmds, cmd)
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Adjust viewport size
		m.logScrollPane.Width = m.width - 2
		m.logScrollPane.Height = m.height/2 - 5
		m.filterInput.Width = m.width - 10

	case metricsMsg:
		m.metrics = msg.metrics
		cmds = append(cmds, m.waitForMetrics)

	case rawLogMsg: // Changed from rawLogMsg.entry
		// Add new log entry, trimming if buffer is too large
		m.logs = append(m.logs, msg.line) // Changed from msg.entry
		if len(m.logs) > maxLogEntries {
			m.logs = m.logs[len(m.logs)-maxLogEntries:]
		}
		m.applyFilter() // Re-apply filter with new logs
		cmds = append(cmds, m.waitForRawLogs) // Continue receiving raw logs

	default:
		// Update spinner and log viewport
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		m.logScrollPane, cmd = m.logScrollPane.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// applyFilter updates m.filteredLogs based on m.currentFilter
func (m *Model) applyFilter() {
	if m.currentFilter == "" {
		m.filteredLogs = m.logs
	} else {
		m.filteredLogs = []string{} // Changed from []types.LogEntry
		for _, entry := range m.logs {
			// Simple string contains for now. Could be regex later.
			if strings.Contains(entry, m.currentFilter) { // Changed from entry.Raw
				m.filteredLogs = append(m.filteredLogs, entry)
			}
		}
	}
	// Update viewport content
	var sb strings.Builder
	for _, entry := range m.filteredLogs {
		sb.WriteString(entry + "\n") // Changed from entry.Raw
	}
	m.logScrollPane.SetContent(sb.String())
	m.logScrollPane.GotoBottom() // Scroll to bottom on new logs/filter applied
}

// View renders the TUI.
func (m Model) View() string {
	var s strings.Builder

	// Top half: Metrics
	// Display spinner and "Waiting for logs..." if no metrics yet
	if m.metrics.TotalRequests == 0 {
		return fmt.Sprintf("\n %s Waiting for logs...\n\n", m.spinner.View())
	}

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FAFAFA")).
		Background(lipgloss.Color("#7D56F4")).
		PaddingLeft(1).
		PaddingRight(1).
		Render("PulseWatch")
	s.WriteString(title)
	s.WriteString("\n\n")

	// Stats
	statsStyle := lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(1)
	stats := fmt.Sprintf(
		"RPS: %.2f | Errors: %.2f%% | Total Requests: %d",
		m.metrics.RPS,
		m.metrics.ErrorRate,
		m.metrics.TotalRequests,
	)
	s.WriteString(statsStyle.Render(stats))
	s.WriteString("\n\n")

	// Latency
	latencyStyle := lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(1)
	latency := fmt.Sprintf(
		"P50: %s | P90: %s | P95: %s | P99: %s",
		m.metrics.P50Latency.Truncate(time.Millisecond),
		m.metrics.P90Latency.Truncate(time.Millisecond),
		m.metrics.P95Latency.Truncate(time.Millisecond),
		m.metrics.P99Latency.Truncate(time.Millisecond),
	)
	s.WriteString(latencyStyle.Render(latency))
	s.WriteString("\n\n")

	// Top Endpoints
	endpointsStyle := lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(1)
	var endpoints strings.Builder
	endpoints.WriteString("Top Endpoints:\n")
	for endpoint, count := range m.metrics.TopEndpoints {
		endpoints.WriteString(fmt.Sprintf("%s: %d\n", endpoint, count))
	}
	s.WriteString(endpointsStyle.Render(endpoints.String()))
	s.WriteString("\n\n")

	// Anomalies
	anomaliesStyle := lipgloss.NewStyle().BorderStyle(lipgloss.RoundedBorder()).Padding(1).Foreground(lipgloss.Color("9"))
	if len(m.metrics.Anomalies) > 0 {
		var anomalies strings.Builder
		anomalies.WriteString("Anomalies:\n")
		for _, anomaly := range m.metrics.Anomalies {
			anomalies.WriteString(fmt.Sprintf("[%s] %s: %s\n", anomaly.Timestamp.Format("15:04:05"), anomaly.Type, anomaly.Message))
		}
		s.WriteString(anomaliesStyle.Render(anomalies.String()))
		s.WriteString("\n")
	}

	// Bottom half: Filter input and Log pane
	s.WriteString(m.filterInput.View())
	s.WriteString("\n")
	s.WriteString(m.logScrollPane.View())

	return s.String()
}


