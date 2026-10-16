package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCLIInformationalFlags(t *testing.T) {
	for _, flag := range []string{"-help", "-version"} {
		var stdout, stderr bytes.Buffer
		if err := run([]string{flag}, &stdout, &stderr); err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		if stdout.Len()+stderr.Len() == 0 {
			t.Errorf("%s produced no output", flag)
		}
	}
}

// Exercise the real signal handler in an isolated process, while a network
// request is stalled. The process must exit normally without a false DOWN event.
func TestSignalShutdown(t *testing.T) {
	if os.Getenv("ISUP_TEST_PROCESS") == "1" {
		err := run([]string{
			"-mode=headless", "-interval=10ms", "-timeout=1m", "-failures=1",
			"-endpoint=" + os.Args[len(os.Args)-2], "-log-dir=" + os.Args[len(os.Args)-1],
		}, os.Stdout, os.Stderr)
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support sending SIGTERM with os.Process.Signal")
	}
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSignalShutdown$", "--", server.URL, t.TempDir())
	cmd.Env = append(os.Environ(), "ISUP_TEST_PROCESS=1")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Error(err)
		}
	case <-ctx.Done():
		t.Error("child did not start a connectivity check")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("shutdown failed: %v\n%s", err, output.String())
	}
	if !strings.Contains(output.String(), "Stopped") || !strings.Contains(output.String(), "notifications=false") || strings.Contains(output.String(), "-> DOWN") {
		t.Fatalf("unexpected shutdown output: %s", output.String())
	}
}

func TestCLIRejectsInvalidConfig(t *testing.T) {
	for _, args := range [][]string{
		{"-interval=0"}, {"-timeout=0"}, {"-failures=0"}, {"-successes=-1"},
		{"-log-retention=0"}, {"-endpoint=ftp://example.com"}, {"unexpected"}, {"-missing-option"},
		{"-mode=invalid"}, {"-mode=terminal"}, {"-dns=example.com"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(args, &stdout, &stderr); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestModeSelection(t *testing.T) {
	for _, tt := range []struct {
		mode         string
		interactive  bool
		wantTerminal bool
		wantError    bool
	}{
		{"auto", true, true, false}, {"auto", false, false, false},
		{"headless", true, false, false}, {"headless", false, false, false},
		{"terminal", true, true, false}, {"terminal", false, false, true},
	} {
		got, err := selectMode(tt.mode, tt.interactive)
		if got != tt.wantTerminal || (err != nil) != tt.wantError {
			t.Errorf("%s interactive=%t: terminal=%t err=%v", tt.mode, tt.interactive, got, err)
		}
	}
}

func TestCustomSources(t *testing.T) {
	if sources := configuredSources(nil, nil); len(sources) < 5 {
		t.Fatal("fewer than five default sources")
	}
	sources := configuredSources([]string{"https://example.net/check"}, []string{"8.8.8.8", "2001:4860:4860::8888", "127.0.0.1:5353"})
	if len(sources) != 4 || sources[0].Name != "example.net" || sources[1].Address != "8.8.8.8:53" || sources[2].Address != "[2001:4860:4860::8888]:53" || sources[3].Address != "127.0.0.1:5353" {
		t.Fatalf("unexpected custom sources: %+v", sources)
	}
}
