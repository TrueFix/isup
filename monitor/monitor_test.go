package monitor

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestMonitorThresholdsAndTransitions(t *testing.T) {
	down := errors.New("offline")
	tests := []struct {
		name    string
		samples []error
		want    []State
	}{
		{"initial offline", []error{down, down, down}, []State{Down}},
		{"initial online", []error{nil, nil, nil}, []State{Up}},
		{"transient failures", []error{nil, nil, down, down, nil, nil}, []State{Up}},
		{"recovery and reset", []error{down, down, nil, down, down, down, nil, down, nil, nil}, []State{Down, Up}},
		{"full cycle", []error{nil, nil, down, down, down, nil, nil, down, down, down}, []State{Up, Down, Up, Down}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			index := 0
			m, err := New(Config{Interval: time.Nanosecond, FailureThreshold: 3, SuccessThreshold: 2}, func(context.Context) error {
				if index == len(tt.samples) {
					cancel()
					return down
				}
				result := tt.samples[index]
				index++
				return result
			})
			if err != nil {
				t.Fatal(err)
			}
			var got []State
			previous := Unknown
			m.Run(ctx, func(event Event) {
				if event.Previous != previous || event.At.IsZero() {
					t.Errorf("invalid event: %+v", event)
				}
				if previous == Unknown && event.Duration != 0 {
					t.Error("initial state has a previous-state duration")
				}
				if (event.Cause != nil) != (event.Current == Down) {
					t.Error("incorrect transition cause")
				}
				previous = event.Current
				got = append(got, event.Current)
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("states %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMonitorCancellationDoesNotReportDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	finished := make(chan struct{})
	m, err := New(Config{Interval: time.Hour, FailureThreshold: 1, SuccessThreshold: 1}, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(finished)
		m.Run(ctx, func(Event) { t.Error("shutdown generated a connectivity event") })
	}()
	<-started
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("monitor did not shut down promptly")
	}
}

func TestMonitorValidation(t *testing.T) {
	check := func(context.Context) error { return nil }
	for _, cfg := range []Config{
		{0, 1, 1}, {time.Second, 0, 1}, {time.Second, 1, 0},
	} {
		if _, err := New(cfg, check); err == nil {
			t.Errorf("accepted invalid config %+v", cfg)
		}
	}
	if _, err := New(Config{time.Second, 1, 1}, nil); err == nil {
		t.Error("accepted a nil checker")
	}
}

func TestLimitedConnectivityNeverBecomesFalseDown(t *testing.T) {
	// Two total failures followed by reachable DNS must reset the DOWN streak.
	states := []State{Up, Up, Down, Down, Degraded, Degraded, Degraded, Down, Down, Down, Up, Up}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	index := 0
	m, err := NewDetailed(Config{Interval: time.Nanosecond, FailureThreshold: 3, SuccessThreshold: 2}, func(context.Context) Report {
		if index == len(states) {
			cancel()
			return Report{State: Down}
		}
		state := states[index]
		index++
		return Report{State: state, At: time.Now()}
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []State
	var snapshots []Snapshot
	m.RunWithUpdates(ctx, func(event Event) { events = append(events, event.Current) }, func(snapshot Snapshot) { snapshots = append(snapshots, snapshot) })
	if want := []State{Up, Degraded, Down, Up}; !reflect.DeepEqual(events, want) {
		t.Fatalf("transitions %v, want %v", events, want)
	}
	if len(snapshots) != len(states) || snapshots[4].Pending != Degraded || snapshots[4].Consecutive != 1 || snapshots[4].State != Up {
		t.Fatalf("candidate state was not reset: %+v", snapshots)
	}
	last := snapshots[len(snapshots)-1]
	if last.Samples != 12 || last.OnlineSamples != 4 {
		t.Fatalf("incorrect sampling statistics: %+v", last)
	}
}
