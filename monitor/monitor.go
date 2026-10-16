package monitor

import (
	"context"
	"errors"
	"time"
)

type State string

const (
	Unknown  State = "UNKNOWN"
	Up       State = "UP"
	Degraded State = "DEGRADED"
	Down     State = "DOWN"
)

type Event struct {
	Previous State
	Current  State
	At       time.Time
	// Duration is the time since the previous confirmed transition, not the
	// exact outage duration (sampling and thresholds introduce detection delay).
	Duration time.Duration
	Cause    error
}

type Config struct {
	Interval         time.Duration
	FailureThreshold int
	SuccessThreshold int
}

type Monitor struct {
	config Config
	sample func(context.Context) Report
}

func New(config Config, check func(context.Context) error) (*Monitor, error) {
	if config.Interval <= 0 || config.FailureThreshold < 1 || config.SuccessThreshold < 1 || check == nil {
		return nil, errors.New("interval and thresholds must be positive and a checker is required")
	}
	return NewDetailed(config, func(ctx context.Context) Report {
		err := check(ctx)
		state := Up
		if err != nil {
			state = Down
		}
		return Report{State: state, Error: err, At: time.Now()}
	})
}

func NewDetailed(config Config, sample func(context.Context) Report) (*Monitor, error) {
	if config.Interval <= 0 || config.FailureThreshold < 1 || config.SuccessThreshold < 1 || sample == nil {
		return nil, errors.New("interval and thresholds must be positive and a sampler is required")
	}
	return &Monitor{config: config, sample: sample}, nil
}

type Snapshot struct {
	Report        Report
	State         State
	Since         time.Time
	Pending       State
	Consecutive   int
	Required      int
	Samples       uint64
	OnlineSamples uint64
}

// Run samples immediately, then at the configured interval, without overlapping
// checks. State belongs exclusively to this goroutine. onChange is called serially
// only for confirmed transitions, including the first UP or DOWN result.
func (m *Monitor) Run(ctx context.Context, onChange func(Event)) {
	m.RunWithUpdates(ctx, onChange, nil)
}

func (m *Monitor) RunWithUpdates(ctx context.Context, onChange func(Event), onSample func(Snapshot)) {
	state := Unknown
	var since time.Time
	pending, consecutive := Unknown, 0
	var samples, onlineSamples uint64
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		report := m.sample(ctx)
		if ctx.Err() != nil {
			return // cancellation is never an outage
		}
		samples++
		if report.State == Up {
			onlineSamples++
		}
		required := m.config.FailureThreshold
		if report.State == Up {
			required = m.config.SuccessThreshold
		}
		if pending != report.State {
			pending, consecutive = report.State, 1
		} else if consecutive < required {
			consecutive++
		}
		next := state
		if consecutive >= required {
			next = pending
		}
		if next != state {
			now := time.Now()
			event := Event{Previous: state, Current: next, At: now, Cause: report.Error}
			if !since.IsZero() {
				event.Duration = now.Sub(since)
			}
			state, since = next, now
			if onChange != nil {
				onChange(event)
			}
		}
		if onSample != nil {
			onSample(Snapshot{Report: report, State: state, Since: since, Pending: pending,
				Consecutive: consecutive, Required: required, Samples: samples, OnlineSamples: onlineSamples})
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
