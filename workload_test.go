package go_loadgen

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

type testRequest struct{}
type testResult struct{}

type testProvider struct{}

func (testProvider) GetData() testRequest { return testRequest{} }

type testCollector struct{ count atomic.Uint64 }

func (c *testCollector) Collect(testResult) { c.count.Add(1) }
func (*testCollector) Close()               {}

func TestRunDrainsRequestsAfterSchedulingEnds(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	client := testClient(func(context.Context, testRequest) testResult {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		return testResult{}
	})
	workload := mustWorkload(t, Spec{
		Duration:  time.Second,
		Endpoints: map[string]Endpoint{"one": mustEndpoint(t, client, testProvider{}, &testCollector{})},
		Phases:    []Phase{{Duration: 10 * time.Millisecond, RPS: 1000, Targets: []Target{{Endpoint: "one", Weight: 1}}}},
	})

	done := make(chan Report, 1)
	go func() { done <- workload.Run(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("workload never issued a request")
	}
	select {
	case <-done:
		t.Fatal("run returned before its issued request completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	report := <-done
	if report.Issued == 0 || report.Completed != report.Issued {
		t.Fatalf("issued=%d completed=%d, want all issued requests drained", report.Issued, report.Completed)
	}
	if report.DrainTimedOut {
		t.Fatal("default drain must not time out")
	}
}

func TestRunCancelsRequestsAfterDrainTimeout(t *testing.T) {
	cancelled := make(chan struct{})
	client := testClient(func(ctx context.Context, _ testRequest) testResult {
		<-ctx.Done()
		select {
		case <-cancelled:
		default:
			close(cancelled)
		}
		return testResult{}
	})
	workload := mustWorkload(t, Spec{
		Duration:     time.Second,
		DrainTimeout: 20 * time.Millisecond,
		Endpoints:    map[string]Endpoint{"one": mustEndpoint(t, client, testProvider{}, &testCollector{})},
		Phases:       []Phase{{Duration: 5 * time.Millisecond, RPS: 1000, Targets: []Target{{Endpoint: "one", Weight: 1}}}},
	})

	report := workload.Run(context.Background())
	select {
	case <-cancelled:
	default:
		t.Fatal("drain timeout did not cancel outstanding requests")
	}
	if !report.DrainTimedOut || report.Completed != report.Issued {
		t.Fatalf("timeout=%t issued=%d completed=%d", report.DrainTimedOut, report.Issued, report.Completed)
	}
}

func TestMaxInFlightDropsWithoutDelayingSchedule(t *testing.T) {
	release := make(chan struct{})
	client := testClient(func(context.Context, testRequest) testResult {
		<-release
		return testResult{}
	})
	workload := mustWorkload(t, Spec{
		Duration:    time.Second,
		MaxInFlight: 2,
		Endpoints:   map[string]Endpoint{"one": mustEndpoint(t, client, testProvider{}, &testCollector{})},
		Phases:      []Phase{{Duration: 10 * time.Millisecond, RPS: 10_000, Targets: []Target{{Endpoint: "one", Weight: 1}}}},
	})

	go func() {
		time.Sleep(30 * time.Millisecond)
		close(release)
	}()
	report := workload.Run(context.Background())
	if report.PeakInFlight != 2 || report.Issued != 2 || report.Dropped == 0 {
		t.Fatalf("peak=%d issued=%d dropped=%d, want capped open-loop issuance", report.PeakInFlight, report.Issued, report.Dropped)
	}
}

func TestAliasChooserRespectsWeights(t *testing.T) {
	first := &countingEndpoint{}
	second := &countingEndpoint{}
	chooser, err := newAliasChooser([]Endpoint{first, second}, []uint32{80, 20})
	if err != nil {
		t.Fatal(err)
	}
	random := phaseRandom{state: 1}
	const samples = 1_000_000
	for range samples {
		chooser.choose(&random).execute(context.Background())
	}
	firstCount := first.count.Load()
	if firstCount < 795_000 || firstCount > 805_000 {
		t.Fatalf("first endpoint received %d/%d, want approximately 80%%", firstCount, samples)
	}
}

func TestNewWorkloadRejectsInvalidDefinitions(t *testing.T) {
	_, err := NewWorkload(Spec{Duration: time.Second, Endpoints: map[string]Endpoint{"one": &countingEndpoint{}}, Phases: []Phase{{Duration: time.Second, RPS: 1, Targets: []Target{{Endpoint: "missing", Weight: 1}}}}})
	if err == nil {
		t.Fatal("expected unknown endpoint validation failure")
	}
}

func TestNewEndpointRejectsTypedNilDependency(t *testing.T) {
	var client *nilTestClient
	endpoint, err := NewEndpoint[testRequest, testResult](client, testProvider{}, &testCollector{})
	if err == nil || endpoint != nil {
		t.Fatalf("endpoint=%v err=%v, want typed nil rejection", endpoint, err)
	}
}

func TestCompiledRampDoesNotRetainCallerPointer(t *testing.T) {
	ramp := &Ramp{To: 100, Step: 10, Every: time.Second}
	workload := mustWorkload(t, Spec{
		Duration:  2 * time.Second,
		Endpoints: map[string]Endpoint{"one": &countingEndpoint{}},
		Phases:    []Phase{{Duration: 2 * time.Second, RPS: 10, Ramp: ramp, Targets: []Target{{Endpoint: "one", Weight: 1}}}},
	})
	ramp.To = 1_000_000
	if got := workload.phases[0].rateAt(time.Second); got != 20 {
		t.Fatalf("compiled ramp rate=%d, want 20 after caller mutation", got)
	}
}

func TestRateAtAndHighRateBatchingDoNotOverflow(t *testing.T) {
	phase := compiledPhase{phase: Phase{RPS: math.MaxUint64 - 10, Ramp: &Ramp{To: math.MaxUint64, Step: 10, Every: time.Second}}}
	if got := phase.rateAt(2 * time.Second); got != math.MaxUint64 {
		t.Fatalf("ramp-up rate=%d, want %d", got, uint64(math.MaxUint64))
	}
	phase.phase = Phase{RPS: math.MaxUint64, Ramp: &Ramp{To: 1, Step: 10, Every: time.Second}}
	if got := phase.rateAt(2 * time.Second); got != math.MaxUint64-20 {
		t.Fatalf("ramp-down rate=%d, want %d", got, uint64(math.MaxUint64-20))
	}

	var remainder uint64
	first := arrivalsForInterval(math.MaxUint64, time.Millisecond, &remainder)
	second := arrivalsForInterval(math.MaxUint64, time.Millisecond, &remainder)
	if first == 0 || second == 0 || remainder >= 1000 {
		t.Fatalf("invalid high-rate batches: first=%d second=%d remainder=%d", first, second, remainder)
	}
}

func TestRunWithCancelledContextDoesNotIssueRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	workload := mustWorkload(t, Spec{
		Duration:  time.Second,
		Endpoints: map[string]Endpoint{"one": &countingEndpoint{}},
		Phases:    []Phase{{Duration: time.Second, RPS: 1000, Targets: []Target{{Endpoint: "one", Weight: 1}}}},
	})
	report := workload.Run(ctx)
	if report.Issued != 0 || report.Completed != 0 {
		t.Fatalf("issued=%d completed=%d after cancellation", report.Issued, report.Completed)
	}
}

type testClient func(context.Context, testRequest) testResult

func (f testClient) CallEndpoint(ctx context.Context, request testRequest) testResult {
	return f(ctx, request)
}

type nilTestClient struct{}

func (*nilTestClient) CallEndpoint(context.Context, testRequest) testResult { return testResult{} }

type countingEndpoint struct{ count atomic.Uint64 }

func (e *countingEndpoint) execute(context.Context) { e.count.Add(1) }

func mustWorkload(t *testing.T, spec Spec) *Workload {
	t.Helper()
	workload, err := NewWorkload(spec)
	if err != nil {
		t.Fatal(err)
	}
	return workload
}

func mustEndpoint[C any, R any](t testing.TB, client Client[C, R], provider DataProvider[C], collector Collector[R]) Endpoint {
	t.Helper()
	endpoint, err := NewEndpoint(client, provider, collector)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}
