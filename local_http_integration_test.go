package go_loadgen

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestLocalHTTPHighLoad(t *testing.T) {
	if testing.Short() || os.Getenv("GO_LOADGEN_HIGH_LOAD") != "1" {
		t.Skip("set GO_LOADGEN_HIGH_LOAD=1 to run the high-load integration test")
	}
	var handled atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		handled.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 4_096
	transport.MaxIdleConnsPerHost = 4_096
	transport.MaxConnsPerHost = 0
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()

	results := &httpResultCollector{}
	const duration = 60 * time.Second
	const rps = 20_000
	workload := mustWorkload(t, Spec{
		Duration: duration,
		Endpoints: map[string]Endpoint{
			"http": mustEndpoint(t, httpLoadClient{client: client, url: server.URL}, testProvider{}, results),
		},
		Phases: []Phase{{Duration: duration, RPS: rps, Targets: []Target{{Endpoint: "http", Weight: 1}}}},
	})

	report := workload.Run(context.Background())
	if report.Scheduled != rps*uint64(duration/time.Second) || report.Issued+report.Missed != report.Scheduled || report.Completed != report.Issued {
		t.Fatalf("scheduled=%d issued=%d missed=%d completed=%d", report.Scheduled, report.Issued, report.Missed, report.Completed)
	}
	if results.failed.Load() != 0 || handled.Load() != report.Issued {
		t.Fatalf("handled=%d failures=%d issued=%d", handled.Load(), results.failed.Load(), report.Issued)
	}
	t.Logf("scheduled=%d issued=%d missed=%d completed=%d schedule=%s issued_rate=%.0f req/s total=%s completed_rate=%.0f req/s peak_in_flight=%d", report.Scheduled, report.Issued, report.Missed, report.Completed, report.SchedulingDuration, float64(report.Issued)/report.SchedulingDuration.Seconds(), report.Duration, float64(report.Completed)/report.Duration.Seconds(), report.PeakInFlight)
}

type httpLoadClient struct {
	client *http.Client
	url    string
}

func (c httpLoadClient) CallEndpoint(ctx context.Context, _ testRequest) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return false
	}
	response, err := c.client.Do(request)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	return response.StatusCode == http.StatusNoContent
}

type httpResultCollector struct {
	completed atomic.Uint64
	failed    atomic.Uint64
}

func (c *httpResultCollector) Collect(ok bool) {
	c.completed.Add(1)
	if !ok {
		c.failed.Add(1)
	}
}

func (*httpResultCollector) Close() {}
