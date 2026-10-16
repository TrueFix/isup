package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/mayankfawkes/isup/monitor"
)

func testModel(t *testing.T) Model {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return New(ctx, cancel, monitor.DefaultSources(), Settings{Interval: 5 * time.Second, Timeout: 4 * time.Second}, make(chan Update))
}

func apply(m Model, msg tea.Msg) Model {
	model, _ := m.Update(msg)
	return model.(Model)
}

func TestResponsiveLayout(t *testing.T) {
	for _, size := range [][2]int{{140, 40}, {100, 24}, {80, 24}, {62, 18}, {40, 14}, {25, 8}, {12, 4}, {1, 1}} {
		m := testModel(t)
		m = apply(m, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Errorf("%v: rendered %d lines", size, len(lines))
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > size[0] {
				t.Errorf("%v: line too wide: %q", size, line)
			}
		}
	}
}

func TestLiveResultsAndConfirmedState(t *testing.T) {
	m := testModel(t)
	m = apply(m, Update{Started: time.Now()})
	if !strings.Contains(ansi.Strip(m.View()), "Checking every source") {
		t.Fatal("missing live checking indicator")
	}
	result := monitor.Result{Index: 0, Source: monitor.DefaultSources()[0], Latency: 20 * time.Millisecond, At: time.Now()}
	m = apply(m, Update{Result: &result})
	if m.rows[0].checking || m.rows[0].result == nil || !m.rows[1].checking {
		t.Fatal("source completion did not update independently")
	}
	m = apply(m, Update{Snapshot: &monitor.Snapshot{State: monitor.Degraded, Pending: monitor.Degraded, Report: monitor.Report{State: monitor.Degraded, Results: []monitor.Result{result}}}})
	if m.checking || !strings.Contains(ansi.Strip(m.View()), "DNS reachable; web access unavailable") {
		t.Fatal("missing partial-connectivity state")
	}
	for i := 0; i < 100; i++ {
		m = apply(m, Update{Log: "activity", Snapshot: &monitor.Snapshot{State: monitor.Up, Report: monitor.Report{State: monitor.Up}}})
	}
	if len(m.history) != 60 || len(m.activity) != 5 {
		t.Fatal("history is not bounded")
	}
}

func TestKeyboardSelectionAndQuit(t *testing.T) {
	m := testModel(t)
	m = apply(m, tea.WindowSizeMsg{Width: 60, Height: 14})
	m = apply(m, tea.KeyMsg{Type: tea.KeyEnd})
	if m.selected != len(m.rows)-1 || !strings.Contains(ansi.Strip(m.View()), "Apple Web") {
		t.Fatal("selected source did not scroll into view")
	}
	m = apply(m, tea.KeyMsg{Type: tea.KeyHome})
	if m.selected != 0 {
		t.Fatal("home did not select the first source")
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if m.ctx.Err() == nil || cmd == nil {
		t.Fatal("quit did not cancel monitoring and exit the UI")
	}
}

func TestUntrustedErrorTextCannotInjectTerminalControls(t *testing.T) {
	m := testModel(t)
	result := monitor.Result{Index: 0, Source: monitor.DefaultSources()[0], Error: errors.New("bad\x1b[2J\x1b]52;c;secret\a\r\nresult")}
	m = apply(m, Update{Result: &result})
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "bad result") || strings.Contains(view, "secret") {
		t.Fatalf("unsafe or missing error text: %q", view)
	}
}
