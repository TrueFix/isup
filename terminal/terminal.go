// Package terminal provides a resize-aware, keyboard-driven connectivity dashboard.
package terminal

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mayankfawkes/isup/monitor"
)

type Update struct {
	Started  time.Time
	Result   *monitor.Result
	Snapshot *monitor.Snapshot
	Event    *monitor.Event
	Log      string
}

type Settings struct {
	Version  string
	Interval time.Duration
	Timeout  time.Duration
	Notify   bool
}

type row struct {
	source   monitor.Source
	result   *monitor.Result
	checking bool
}

type Model struct {
	ctx      context.Context
	stop     context.CancelFunc
	updates  <-chan Update
	settings Settings
	rows     []row
	width    int
	height   int
	selected int
	frame    int
	started  time.Time
	now      time.Time
	checking bool
	lastRun  time.Time
	snapshot monitor.Snapshot
	history  []monitor.State
	activity []string
}

type tick time.Time
type stopped struct{}

func New(ctx context.Context, stop context.CancelFunc, sources []monitor.Source, settings Settings, updates <-chan Update) Model {
	rows := make([]row, len(sources))
	for i, source := range sources {
		rows[i].source = source
	}
	now := time.Now()
	return Model{ctx: ctx, stop: stop, updates: updates, settings: settings, rows: rows,
		width: 80, height: 24, started: now, now: now, snapshot: monitor.Snapshot{State: monitor.Unknown}}
}

func Run(ctx context.Context, stop context.CancelFunc, input io.Reader, output io.Writer, sources []monitor.Source, settings Settings, updates <-chan Update) error {
	p := tea.NewProgram(New(ctx, stop, sources, settings, updates), tea.WithInput(input), tea.WithOutput(output),
		tea.WithAltScreen(), tea.WithContext(ctx), tea.WithoutSignalHandler())
	_, err := p.Run()
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func wait(ctx context.Context, updates <-chan Update) tea.Cmd {
	return func() tea.Msg {
		select {
		case <-ctx.Done():
			return stopped{}
		case update, ok := <-updates:
			if !ok {
				return stopped{}
			}
			return update
		}
	}
}

func pulse() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(now time.Time) tea.Msg { return tick(now) })
}

func (m Model) Init() tea.Cmd { return tea.Batch(wait(m.ctx, m.updates), pulse()) }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.stop()
			return m, tea.Quit
		case "down", "j":
			m.selected = min(len(m.rows)-1, m.selected+1)
		case "up", "k":
			m.selected = max(0, m.selected-1)
		case "pgdown":
			m.selected = min(len(m.rows)-1, m.selected+5)
		case "pgup":
			m.selected = max(0, m.selected-5)
		case "home":
			m.selected = 0
		case "end":
			m.selected = len(m.rows) - 1
		}
	case tick:
		m.now, m.frame = time.Time(msg), m.frame+1
		return m, pulse()
	case stopped:
		return m, tea.Quit
	case Update:
		if !msg.Started.IsZero() {
			m.checking, m.lastRun = true, msg.Started
			for i := range m.rows {
				m.rows[i].checking = true
			}
		}
		if result := msg.Result; result != nil && result.Index >= 0 && result.Index < len(m.rows) {
			m.rows[result.Index].result = result
			m.rows[result.Index].checking = false
		}
		if msg.Snapshot != nil {
			m.snapshot, m.checking = *msg.Snapshot, false
			for _, result := range msg.Snapshot.Report.Results {
				if result.Index >= 0 && result.Index < len(m.rows) {
					copy := result
					m.rows[result.Index].result, m.rows[result.Index].checking = &copy, false
				}
			}
			m.history = append(m.history, msg.Snapshot.Report.State)
			if len(m.history) > 60 {
				m.history = m.history[len(m.history)-60:]
			}
		}
		if msg.Event != nil {
			m.addActivity(fmt.Sprintf("%s  %s → %s", msg.Event.At.Format("15:04:05"), msg.Event.Previous, msg.Event.Current))
		}
		if msg.Log != "" {
			m.addActivity(clean(msg.Log))
		}
		return m, wait(m.ctx, m.updates)
	}
	return m, nil
}

func (m *Model) addActivity(text string) {
	m.activity = append(m.activity, text)
	if len(m.activity) > 5 {
		m.activity = m.activity[len(m.activity)-5:]
	}
}

var (
	muted = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "242", Dark: "245"})
	brand = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "25", Dark: "81"})
	good  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "84"})
	bad   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "160", Dark: "203"})
	warn  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "130", Dark: "221"})
)

func stateText(state monitor.State) string {
	switch state {
	case monitor.Up:
		return good.Render(string(state))
	case monitor.Down:
		return bad.Render(string(state))
	case monitor.Degraded:
		return warn.Render(string(state))
	default:
		return muted.Render(string(monitor.Unknown))
	}
}

func clean(s string) string {
	s = ansi.Strip(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func cell(s string, width int) string {
	s = ansi.Truncate(s, max(0, width), "…")
	return s + strings.Repeat(" ", max(0, width-ansi.StringWidth(s)))
}

func (m Model) sourceLine(i int) string {
	r := m.rows[i]
	status, latency := muted.Render("WAIT"), "—"
	if r.result != nil {
		status = good.Render("OK")
		latency = r.result.Latency.Round(time.Millisecond).String()
		if r.result.Error != nil {
			status, latency = bad.Render("FAIL"), "—"
		}
	}
	if r.checking {
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		status = brand.Render(frames[m.frame%len(frames)] + " CHECK")
	}
	prefix := "  "
	if i == m.selected {
		prefix = brand.Render("› ")
	}
	name, address := clean(r.source.Name), clean(r.source.Address)
	protocol := string(r.source.Protocol)
	if strings.HasPrefix(r.source.Address, "http://") {
		protocol = "HTTP"
	}
	w := m.width - 2
	switch {
	case m.width >= 90:
		return prefix + cell(name, 19) + cell(protocol, 7) + cell(address, w-19-7-10-10) + cell(status, 10) + cell(latency, 10)
	case m.width >= 62:
		return prefix + cell(name, 18) + cell(protocol, 7) + cell(address, w-18-7-9-8) + cell(status, 9) + cell(latency, 8)
	case m.width >= 38:
		return prefix + cell(name, w-7-9-8) + cell(protocol, 7) + cell(status, 9) + cell(latency, 8)
	default:
		return prefix + cell(name, max(1, w-9)) + cell(status, 9)
	}
}

func (m Model) sourceHeader() string {
	w := m.width - 2
	switch {
	case m.width >= 90:
		return "  " + cell("SOURCE", 19) + cell("TYPE", 7) + cell("TARGET", w-19-7-10-10) + cell("STATUS", 10) + "TIME"
	case m.width >= 62:
		return "  " + cell("SOURCE", 18) + cell("TYPE", 7) + cell("TARGET", w-18-7-9-8) + cell("STATUS", 9) + "TIME"
	case m.width >= 38:
		return "  " + cell("SOURCE", w-7-9-8) + cell("TYPE", 7) + cell("STATUS", 9) + "TIME"
	default:
		return "  " + cell("SOURCE", max(1, w-9)) + "STATUS"
	}
}

func (m Model) View() string {
	notify := "off"
	if m.settings.Notify {
		notify = "on"
	}
	lines := []string{brand.Render("ISUP") + "  " + muted.Render("NETWORK OBSERVATORY "+clean(m.settings.Version)),
		muted.Render(fmt.Sprintf("%d sources · interval %s · deadline %s · notifications %s", len(m.rows), m.settings.Interval, m.settings.Timeout, notify)), ""}
	description := "Establishing connectivity…"
	switch m.snapshot.State {
	case monitor.Up:
		description = "Internet reachable"
	case monitor.Degraded:
		description = "DNS reachable; web access unavailable"
	case monitor.Down:
		description = "All sources failed — outage confirmed"
	}
	lines = append(lines, stateText(m.snapshot.State)+"  "+description)
	responding := m.snapshot.Report.Successes()
	if m.checking {
		responding = 0
		for _, row := range m.rows {
			if !row.checking && row.result != nil && row.result.Error == nil {
				responding++
			}
		}
	}
	summary := fmt.Sprintf("%d/%d responding", responding, len(m.rows))
	if m.snapshot.Samples > 0 {
		summary += fmt.Sprintf(" · %.1f%% online samples", float64(m.snapshot.OnlineSamples)/float64(m.snapshot.Samples)*100)
	}
	if !m.snapshot.Since.IsZero() {
		summary += " · state for " + m.now.Sub(m.snapshot.Since).Round(time.Second).String()
	}
	lines = append(lines, summary)
	progress := "Waiting for first check"
	if m.checking {
		progress = brand.Render("Checking every source…")
	} else if !m.lastRun.IsZero() {
		remaining := max(time.Duration(0), m.settings.Interval-m.now.Sub(m.lastRun)).Round(time.Second)
		progress = fmt.Sprintf("Next check in %s", remaining)
	}
	if m.snapshot.Pending != "" && m.snapshot.Pending != m.snapshot.State {
		progress += fmt.Sprintf(" · confirming %s %d/%d", m.snapshot.Pending, m.snapshot.Consecutive, m.snapshot.Required)
	}
	lines = append(lines, progress)
	if m.height >= 18 {
		history := ""
		start := max(0, len(m.history)-max(1, m.width-18))
		for _, state := range m.history[start:] {
			switch state {
			case monitor.Up:
				history += good.Render("●")
			case monitor.Degraded:
				history += warn.Render("◆")
			default:
				history += bad.Render("×")
			}
		}
		lines = append(lines, muted.Render("RECENT SAMPLES  ")+history)
	}
	lines = append(lines, "", muted.Render(m.sourceHeader()))
	// Reserve detail/help space; scroll the source list around the selected row.
	reserve := 4
	if m.height >= 22 {
		reserve += 2
	}
	visible := max(1, min(len(m.rows), m.height-len(lines)-reserve))
	first := max(0, min(m.selected-visible+1, len(m.rows)-visible))
	for i := first; i < min(len(m.rows), first+visible); i++ {
		lines = append(lines, m.sourceLine(i))
	}
	if len(m.rows) > 0 && m.selected >= 0 {
		r := m.rows[m.selected]
		lines = append(lines, "", muted.Render(clean(r.source.Address)))
		detail := "Waiting for a result"
		if r.result != nil {
			detail = fmt.Sprintf("OK · %s · checked %s", r.result.Latency.Round(time.Millisecond), r.result.At.Format("15:04:05"))
			if r.result.Error != nil {
				detail = "FAIL · " + clean(r.result.Error.Error())
			}
		}
		lines = append(lines, detail)
	}
	if m.height >= 22 && len(m.activity) > 0 {
		lines = append(lines, "", muted.Render(m.activity[len(m.activity)-1]))
	}
	foot := muted.Render(fmt.Sprintf("↑/↓ select · q quit · %s elapsed", m.now.Sub(m.started).Round(time.Second)))
	if m.height < 10 {
		// Compact screens prioritize current state and selected source.
		lines = []string{brand.Render("ISUP") + "  " + stateText(m.snapshot.State), progress}
		if len(m.rows) > 0 {
			lines = append(lines, m.sourceLine(max(0, m.selected)))
		}
	}
	if len(lines) >= m.height {
		lines = lines[:max(0, m.height-1)]
	}
	lines = append(lines, foot)
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], m.width, "…")
	}
	return strings.Join(lines, "\n")
}
