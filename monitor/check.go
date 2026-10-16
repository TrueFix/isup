// Package monitor checks Internet reachability without depending on a single provider.
package monitor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Protocol string

const (
	Web Protocol = "HTTPS"
	DNS Protocol = "DNS"
)

type Source struct {
	Name           string
	Address        string
	Protocol       Protocol
	ExpectedStatus int
	ExpectedBody   string
}

// DefaultSources span five providers and two protocols. DNS tests use the
// literal public resolver IPs, independently of the machine's configured DNS.
func DefaultSources() []Source {
	return []Source{
		{Name: "Cloudflare DNS", Address: "1.1.1.1:53", Protocol: DNS},
		{Name: "Google DNS", Address: "8.8.8.8:53", Protocol: DNS},
		{Name: "Quad9 DNS", Address: "9.9.9.9:53", Protocol: DNS},
		{Name: "Cloudflare Web", Address: "https://cp.cloudflare.com/generate_204", Protocol: Web, ExpectedStatus: 204},
		{Name: "Google Web", Address: "https://www.gstatic.com/generate_204", Protocol: Web, ExpectedStatus: 204},
		{Name: "Mozilla Web", Address: "https://detectportal.firefox.com/success.txt", Protocol: Web, ExpectedStatus: 200, ExpectedBody: "success"},
		{Name: "Apple Web", Address: "https://www.apple.com/library/test/success.html", Protocol: Web, ExpectedStatus: 200, ExpectedBody: "<HTML><HEAD><TITLE>Success</TITLE></HEAD><BODY>Success</BODY></HTML>"},
	}
}

// DefaultEndpoints is the original 204-only endpoint list for API compatibility.
// The application uses DefaultSources, which also includes DNS and content checks.
func DefaultEndpoints() []string {
	return []string{"https://www.gstatic.com/generate_204", "https://cp.cloudflare.com/generate_204"}
}

type Result struct {
	Index   int
	Source  Source
	Latency time.Duration
	At      time.Time
	Error   error
}

type Report struct {
	Results []Result
	At      time.Time
	State   State
	Error   error
}

func (r Report) Successes() int {
	n := 0
	for _, result := range r.Results {
		if result.Error == nil {
			n++
		}
	}
	return n
}

type Checker struct {
	client    *http.Client
	transport *http.Transport
	sources   []Source
	timeout   time.Duration
}

// NewChecker configures custom URLs that must return HTTP 204.
func NewChecker(endpoints []string, timeout time.Duration) (*Checker, error) {
	sources := make([]Source, len(endpoints))
	for i, endpoint := range endpoints {
		sources[i] = Source{Name: endpoint, Address: endpoint, Protocol: Web, ExpectedStatus: 204}
	}
	return NewSourceChecker(sources, timeout)
}

func NewSourceChecker(sources []Source, timeout time.Duration) (*Checker, error) {
	if len(sources) == 0 || len(sources) > 16 {
		return nil, errors.New("provide between 1 and 16 sources")
	}
	if timeout <= 0 {
		return nil, errors.New("timeout must be positive")
	}
	seen := make(map[string]bool)
	for _, source := range sources {
		if seen[string(source.Protocol)+source.Address] {
			return nil, fmt.Errorf("duplicate source: %s", source.Address)
		}
		seen[string(source.Protocol)+source.Address] = true
		switch source.Protocol {
		case Web:
			u, err := url.Parse(source.Address)
			if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
				return nil, fmt.Errorf("invalid HTTP(S) endpoint: %q", source.Address)
			}
			if source.ExpectedStatus != 204 && (source.ExpectedStatus != 200 || source.ExpectedBody == "") {
				return nil, errors.New("web sources require HTTP 204 or HTTP 200 with an exact expected body")
			}
		case DNS:
			host, port, err := net.SplitHostPort(source.Address)
			if err != nil || net.ParseIP(host) == nil || port == "" {
				return nil, fmt.Errorf("invalid DNS resolver address (expected IP:port): %q", source.Address)
			}
			if number, err := net.LookupPort("udp", port); err != nil || number < 1 {
				return nil, fmt.Errorf("invalid DNS resolver port: %q", port)
			}
		default:
			return nil, fmt.Errorf("unsupported source protocol: %q", source.Protocol)
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	return &Checker{
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		transport: transport,
		sources:   append([]Source(nil), sources...),
		timeout:   timeout,
	}, nil
}

func (c *Checker) Close() { c.transport.CloseIdleConnections() }

func (c *Checker) Check(ctx context.Context) error {
	report := c.Sample(ctx, nil)
	if report.State != Down {
		return nil
	}
	return report.Error
}

// Sample checks every source in parallel under one deadline. It does not stop
// after the first success: the dashboard needs real results for every source.
// onResult is called serially as probes finish. All probe goroutines are joined.
func (c *Checker) Sample(ctx context.Context, onResult func(Result)) Report {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	results := make(chan Result, len(c.sources))
	var workers sync.WaitGroup
	for i, source := range c.sources {
		workers.Add(1)
		go func(i int, source Source) {
			defer workers.Done()
			start := time.Now()
			err := c.probe(ctx, source)
			results <- Result{Index: i, Source: source, Latency: time.Since(start), At: time.Now(), Error: err}
		}(i, source)
	}
	report := Report{Results: make([]Result, len(c.sources))}
	for range c.sources {
		result := <-results
		report.Results[result.Index] = result
		if onResult != nil {
			onResult(result)
		}
	}
	workers.Wait()
	report.At = time.Now()
	report.State, report.Error = classify(report.Results)
	return report
}

func classify(results []Result) (State, error) {
	webConfigured, webOK, dnsOK := false, false, false
	var failures []error
	for _, result := range results {
		webConfigured = webConfigured || result.Source.Protocol == Web
		if result.Error != nil {
			failures = append(failures, fmt.Errorf("%s: %w", result.Source.Name, result.Error))
		} else if result.Source.Protocol == Web {
			webOK = true
		} else if result.Source.Protocol == DNS {
			dnsOK = true
		}
	}
	if webOK || (!webConfigured && dnsOK) {
		return Up, nil
	}
	if dnsOK {
		return Degraded, errors.Join(failures...)
	}
	return Down, errors.Join(failures...)
}

func (c *Checker) probe(ctx context.Context, source Source) error {
	if source.Protocol == DNS {
		return probeDNS(ctx, source.Address)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source.Address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Cache-Control", "no-cache, no-store")
	req.Header.Set("User-Agent", "IsUp/2")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != source.ExpectedStatus {
		return fmt.Errorf("expected HTTP %d, got %s", source.ExpectedStatus, resp.Status)
	}
	if source.ExpectedBody != "" {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
		if err != nil {
			return err
		}
		if len(body) > 4096 || strings.TrimSpace(string(body)) != source.ExpectedBody {
			return errors.New("unexpected response content (possible captive portal)")
		}
	}
	return nil
}
