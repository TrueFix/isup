package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gen2brain/beeep"
	"github.com/mayankfawkes/isup/logging"
	"github.com/mayankfawkes/isup/monitor"
	"github.com/mayankfawkes/isup/terminal"
	"golang.org/x/term"
)

//go:embed images/*.png
var images embed.FS

var version = "dev"

type endpoints []string

func (e *endpoints) String() string { return fmt.Sprint([]string(*e)) }
func (e *endpoints) Set(value string) error {
	*e = append(*e, value)
	return nil
}

type updateWriter struct{ publish func(terminal.Update) }

func (w updateWriter) Write(p []byte) (int, error) {
	w.publish(terminal.Update{Log: string(p)})
	return len(p), nil
}

func selectMode(mode string, interactive bool) (bool, error) {
	switch mode {
	case "auto":
		return interactive, nil
	case "terminal":
		if !interactive {
			return false, errors.New("terminal mode requires an interactive input and output terminal; use -mode=headless for pipes or services")
		}
		return true, nil
	case "headless":
		return false, nil
	default:
		return false, errors.New("mode must be auto, terminal, or headless")
	}
}

func isInteractive(stdout io.Writer) bool {
	file, ok := stdout.(interface{ Fd() uintptr })
	return ok && term.IsTerminal(int(file.Fd())) && term.IsTerminal(int(os.Stdin.Fd())) && os.Getenv("TERM") != "dumb"
}

func configuredSources(urls, resolvers []string) []monitor.Source {
	if len(urls)+len(resolvers) == 0 {
		return monitor.DefaultSources()
	}
	sources := make([]monitor.Source, 0, len(urls)+len(resolvers))
	for _, endpoint := range urls {
		name := endpoint
		if u, err := url.Parse(endpoint); err == nil && u.Hostname() != "" {
			name = u.Hostname()
		}
		sources = append(sources, monitor.Source{Name: name, Address: endpoint, Protocol: monitor.Web, ExpectedStatus: 204})
	}
	for _, resolver := range resolvers {
		address := resolver
		if net.ParseIP(resolver) != nil {
			address = net.JoinHostPort(resolver, "53")
		}
		sources = append(sources, monitor.Source{Name: "DNS " + resolver, Address: address, Protocol: monitor.DNS})
	}
	return sources
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "isup:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("isup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	interval := flags.Duration("interval", 5*time.Second, "time between connectivity checks")
	timeout := flags.Duration("timeout", 4*time.Second, "maximum duration of a connectivity check")
	failures := flags.Int("failures", 3, "consecutive failed checks before reporting DOWN")
	successes := flags.Int("successes", 2, "consecutive successful checks before reporting UP")
	mode := flags.String("mode", "auto", "auto, terminal (live dashboard), or headless (plain logs)")
	notify := flags.Bool("notify", false, "desktop notifications (default: on in terminal mode, off in headless mode)")
	verbose := flags.Bool("verbose", false, "log every source result after each completed sample")
	logDir := flags.String("log-dir", "", "log directory (default: user cache directory/isup)")
	retention := flags.Int("log-retention", 30, "number of days of logs to retain")
	showVersion := flags.Bool("version", false, "print version and exit")
	var urls, resolvers endpoints
	flags.Var(&urls, "endpoint", "HTTP(S) URL returning HTTP 204; repeat to replace the default providers")
	flags.Var(&resolvers, "dns", "DNS resolver IP or IP:port; repeat (custom sources replace all defaults)")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *showVersion {
		fmt.Fprintln(stdout, "isup", version)
		return nil
	}
	terminalMode, err := selectMode(*mode, isInteractive(stdout))
	if err != nil {
		return err
	}
	notifyExplicit := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "notify" {
			notifyExplicit = true
		}
	})
	if !notifyExplicit {
		*notify = terminalMode
	}
	if *retention < 1 {
		return errors.New("log-retention must be positive")
	}
	if *interval <= 0 || *failures < 1 || *successes < 1 {
		return errors.New("interval, failures and successes must be positive")
	}
	sources := configuredSources(urls, resolvers)
	checker, err := monitor.NewSourceChecker(sources, *timeout)
	if err != nil {
		return err
	}
	defer checker.Close()
	if *logDir == "" {
		*logDir, err = logging.DefaultDir()
		if err != nil {
			fmt.Fprintln(stderr, "isup: file logging unavailable:", err)
		}
	}
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(signalContext)
	defer cancel()
	updates := make(chan terminal.Update, 128)
	// Display updates must not delay probing, even on a slow or blocked terminal.
	// Each full sample refreshes all source rows if an intermediate update is lost.
	publish := func(update terminal.Update) {
		if !terminalMode {
			return
		}
		select {
		case updates <- update:
		case <-ctx.Done():
		default:
		}
	}
	console := stdout
	if terminalMode {
		console = updateWriter{publish: publish}
	}
	logger := log.New(console, "", log.LstdFlags)
	// A logging failure should not disable monitoring. Report it to stderr.
	var logFile *os.File
	logDay := ""
	defer func() {
		logger.SetOutput(console)
		if logFile != nil {
			logFile.Close()
		}
	}()
	rotateLog := func(now time.Time) {
		if *logDir == "" {
			return
		}
		day := now.Format("2006-01-02")
		if day == logDay {
			return
		}
		logDay = day
		logger.SetOutput(console)
		if logFile != nil {
			logFile.Close()
			logFile = nil
		}
		file, err := logging.Open(*logDir, *retention, now)
		if err != nil {
			if terminalMode {
				logger.Printf("File logging unavailable: %v", err)
			} else {
				fmt.Fprintln(stderr, "isup: file logging unavailable:", err)
			}
			return
		}
		logFile = file
		logger.SetOutput(io.MultiWriter(console, logFile))
	}
	rotateLog(time.Now())
	var sendNotification func(monitor.Event)
	if *notify {
		sendNotification = notifications(ctx, *logDir, logger)
	}
	logger.Printf("IsUp %s started; interval=%s timeout=%s failures=%d successes=%d sources=%d notifications=%t", version, *interval, *timeout, *failures, *successes, len(sources), *notify)
	// Rotation runs on every sample, even when there are no state changes.
	watcher, err := monitor.NewDetailed(monitor.Config{
		Interval: *interval, FailureThreshold: *failures, SuccessThreshold: *successes,
	}, func(ctx context.Context) monitor.Report {
		rotateLog(time.Now())
		publish(terminal.Update{Started: time.Now()})
		return checker.Sample(ctx, func(result monitor.Result) {
			publish(terminal.Update{Result: &result})
		})
	})
	if err != nil {
		return err
	}
	onChange := func(event monitor.Event) {
		logger.Printf("Internet status %s -> %s; previous state duration=%s", event.Previous, event.Current, event.Duration.Round(time.Millisecond))
		if event.Cause != nil {
			logger.Printf("Connectivity checks failed: %v", event.Cause)
		}
		if sendNotification != nil {
			sendNotification(event)
		}
		publish(terminal.Update{Event: &event})
	}
	onSample := func(snapshot monitor.Snapshot) {
		publish(terminal.Update{Snapshot: &snapshot})
		if *verbose {
			logger.Printf("Sample: %s; confirmed=%s; responding=%d/%d; confirming=%s %d/%d", snapshot.Report.State, snapshot.State, snapshot.Report.Successes(), len(sources), snapshot.Pending, snapshot.Consecutive, snapshot.Required)
			for _, result := range snapshot.Report.Results {
				if result.Error == nil {
					logger.Printf("Source %s [%s] OK %s", result.Source.Name, result.Source.Address, result.Latency.Round(time.Millisecond))
				} else {
					logger.Printf("Source %s [%s] FAIL: %v", result.Source.Name, result.Source.Address, result.Error)
				}
			}
		}
	}
	if terminalMode {
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			watcher.RunWithUpdates(ctx, onChange, onSample)
		}()
		err = terminal.Run(ctx, cancel, os.Stdin, stdout, sources, terminal.Settings{
			Version: version, Interval: *interval, Timeout: *timeout, Notify: *notify,
		}, updates)
		cancel()
		<-finished
		console = stdout
		logger.SetOutput(stdout)
		if logFile != nil {
			logger.SetOutput(io.MultiWriter(stdout, logFile))
		}
	} else {
		watcher.RunWithUpdates(ctx, onChange, onSample)
	}
	logger.Println("Stopped")
	return err
}

// Desktop integrations can block or be unavailable. Keep them off the sampling
// path, bound the queue, and never wait for the desktop service during shutdown.
func notifications(ctx context.Context, dir string, logger *log.Logger) func(monitor.Event) {
	beeep.AppName = "IsUp"
	icons := make(map[monitor.State]string)
	for state, name := range map[monitor.State]string{monitor.Up: "connected.png", monitor.Down: "disconnected.png"} {
		if dir == "" {
			break
		}
		data, err := images.ReadFile("images/" + name)
		if err == nil {
			path := filepath.Join(dir, name)
			if err = os.WriteFile(path, data, 0600); err == nil {
				icons[state] = path
			}
		}
		if err != nil {
			logger.Printf("Notification icon unavailable: %v", err)
		}
	}
	queue := make(chan monitor.Event, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-queue:
				if ctx.Err() != nil {
					return
				}
				title, message := "Internet Up", "Internet reachability has been restored."
				if event.Previous == monitor.Unknown {
					message = "Internet reachability has been established."
				}
				if event.Current == monitor.Down {
					title, message = "Internet Down", "All configured connectivity sources failed repeatedly."
				} else if event.Current == monitor.Degraded {
					title, message = "Limited Connectivity", "Public DNS is reachable, but all configured web connectivity checks failed."
				}
				if err := beeep.Notify(title, message, icons[event.Current]); err != nil {
					logger.Printf("Desktop notification failed: %v", err)
				}
			}
		}
	}()
	return func(event monitor.Event) {
		select {
		case queue <- event:
		default:
			// Replace a stale queued event with the latest confirmed state.
			select {
			case <-queue:
			default:
			}
			select {
			case queue <- event:
			default:
			}
		}
	}
}
