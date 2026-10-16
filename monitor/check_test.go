package monitor

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func newTestChecker(t *testing.T, endpoints []string, timeout time.Duration) *Checker {
	t.Helper()
	c, err := NewChecker(endpoints, timeout)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestCheckResponses(t *testing.T) {
	for _, status := range []int{204, 200, 302, 403, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var redirected atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login" {
					redirected.Store(true)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Location", "/login")
				w.WriteHeader(status)
			}))
			defer server.Close()
			c := newTestChecker(t, []string{server.URL}, time.Second)
			err := c.Check(context.Background())
			if (err == nil) != (status == 204) {
				t.Fatalf("status %d: error=%v", status, err)
			}
			if redirected.Load() {
				t.Fatal("followed a captive-portal redirect")
			}
		})
	}
}

func TestSampleReportsEveryProviderWithinDeadline(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer stalled.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer healthy.Close()
	c := newTestChecker(t, []string{stalled.URL, healthy.URL}, 150*time.Millisecond)
	var completed []Result
	report := c.Sample(context.Background(), func(result Result) { completed = append(completed, result) })
	if report.State != Up || report.Successes() != 1 || len(completed) != 2 {
		t.Fatalf("incorrect partial-success report: %+v; callbacks=%d", report, len(completed))
	}
	if completed[0].Source.Address != healthy.URL || report.Results[0].Error == nil || report.Results[1].Error != nil {
		t.Fatalf("did not receive live success before the stalled provider timed out: %+v", report)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("stalled provider was not canceled at the shared deadline")
	}
}

func TestContentChecksRejectCaptivePortals(t *testing.T) {
	for _, body := range []string{"success\n", "<html>Sign in to Wi-Fi</html>", "success" + string(make([]byte, 5000))} {
		t.Run(body[:min(len(body), 20)], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(body))
			}))
			defer server.Close()
			checker, err := NewSourceChecker([]Source{{Name: "content", Address: server.URL, Protocol: Web, ExpectedStatus: 200, ExpectedBody: "success"}}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer checker.Close()
			if got := checker.Sample(context.Background(), nil).State; (got == Up) != (body == "success\n") {
				t.Fatalf("body %q produced %s", body[:min(len(body), 30)], got)
			}
		})
	}
}

func TestMultiProtocolClassification(t *testing.T) {
	failure := errors.New("unreachable")
	tests := []struct {
		name    string
		results []Result
		want    State
	}{
		{"all fail", []Result{{Source: Source{Protocol: Web}, Error: failure}, {Source: Source{Protocol: DNS}, Error: failure}}, Down},
		{"web succeeds despite blocked DNS", []Result{{Source: Source{Protocol: Web}}, {Source: Source{Protocol: DNS}, Error: failure}}, Up},
		{"DNS only with web failure is limited", []Result{{Source: Source{Protocol: Web}, Error: failure}, {Source: Source{Protocol: DNS}}}, Degraded},
		{"custom DNS only configuration", []Result{{Source: Source{Protocol: DNS}}}, Up},
		{"one provider failing is not an outage", []Result{{Source: Source{Protocol: Web}, Error: failure}, {Source: Source{Protocol: Web}}}, Up},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state, err := classify(tt.results)
			if state != tt.want || (err == nil) != (tt.want == Up) {
				t.Fatalf("got %s, %v; want %s", state, err, tt.want)
			}
		})
	}
}

func TestDNSProbesValidateReplies(t *testing.T) {
	for _, ip := range [][4]byte{{93, 184, 216, 34}, {10, 0, 0, 1}} {
		t.Run(net.IP(ip[:]).String(), func(t *testing.T) {
			conn, err := net.ListenPacket("udp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				buffer := make([]byte, 4096)
				for {
					n, address, err := conn.ReadFrom(buffer)
					if err != nil {
						return
					}
					var query dnsmessage.Message
					if query.Unpack(buffer[:n]) != nil || len(query.Questions) != 1 {
						continue
					}
					answer := dnsmessage.Message{Header: dnsmessage.Header{ID: query.ID, Response: true, RecursionDesired: true, RecursionAvailable: true}, Questions: query.Questions,
						Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: query.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: ip}}}}
					data, err := answer.Pack()
					if err == nil {
						conn.WriteTo(data, address)
					}
				}
			}()
			defer func() { conn.Close(); <-finished }()
			checker, err := NewSourceChecker([]Source{{Name: "test DNS", Address: conn.LocalAddr().String(), Protocol: DNS}}, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer checker.Close()
			want := Up
			if ip[0] == 10 {
				want = Down
			}
			if report := checker.Sample(context.Background(), nil); report.State != want {
				t.Fatalf("got %s, want %s: %v", report.State, want, report.Error)
			}
		})
	}
}

func TestInvalidSourceConfigurations(t *testing.T) {
	for _, sources := range [][]Source{
		{{Protocol: DNS, Address: "example.com:53"}},
		{{Protocol: DNS, Address: "8.8.8.8"}},
		{{Protocol: DNS, Address: "8.8.8.8:bad"}},
		{{Protocol: "PING", Address: "1.1.1.1"}},
		{{Protocol: Web, Address: "https://example.com", ExpectedStatus: 200}},
		{{Protocol: DNS, Address: "8.8.8.8:53"}, {Protocol: DNS, Address: "8.8.8.8:53"}},
	} {
		if checker, err := NewSourceChecker(sources, time.Second); err == nil {
			checker.Close()
			t.Errorf("accepted invalid configuration: %+v", sources)
		}
	}
}

func TestCheckTimeoutAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	c := newTestChecker(t, []string{server.URL}, 50*time.Millisecond)
	start := time.Now()
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("stalled endpoint counted as reachable")
	}
	if time.Since(start) > time.Second {
		t.Fatal("check exceeded its deadline")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Check(ctx); err == nil {
		t.Fatal("canceled check succeeded")
	}
}

func TestCheckRejectsUntrustedTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	c := newTestChecker(t, []string{server.URL}, time.Second)
	if err := c.Check(context.Background()); err == nil {
		t.Fatal("untrusted TLS certificate counted as reachable")
	}
}

func TestCheckerValidation(t *testing.T) {
	for _, endpoint := range []string{"", "://bad", "ftp://example.com", "https:///path", "https://user:pass@example.com", "https://example.com/#fragment"} {
		if c, err := NewChecker([]string{endpoint}, time.Second); err == nil {
			c.Close()
			t.Errorf("accepted invalid endpoint %q", endpoint)
		}
	}
	if _, err := NewChecker(nil, time.Second); err == nil {
		t.Error("accepted no endpoints")
	}
	if _, err := NewChecker(DefaultEndpoints(), 0); err == nil {
		t.Error("accepted an unbounded timeout")
	}
}
