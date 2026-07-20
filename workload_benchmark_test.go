package go_loadgen

import (
	"context"
	"testing"
	"time"
)

func BenchmarkAliasChooser(b *testing.B) {
	chooser, err := newAliasChooser(
		[]Endpoint{&countingEndpoint{}, &countingEndpoint{}, &countingEndpoint{}},
		[]uint32{70, 20, 10},
	)
	if err != nil {
		b.Fatal(err)
	}
	random := phaseRandom{state: 1}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		chooser.choose(&random)
	}
}

func BenchmarkEndpointAdapter(b *testing.B) {
	endpoint := mustEndpoint(b, testClient(func(context.Context, testRequest) testResult { return testResult{} }), testProvider{}, &testCollector{})
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		endpoint.execute(ctx)
	}
}

func BenchmarkWorkloadRun100kRPS(b *testing.B) {
	workload := mustBenchmarkWorkload(b, 100_000, time.Second)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		report := workload.Run(ctx)
		if report.Completed != report.Issued || report.Scheduled != report.Issued+report.Missed {
			b.Fatalf("scheduled=%d issued=%d completed=%d", report.Scheduled, report.Issued, report.Completed)
		}
		b.ReportMetric(float64(report.Issued), "issued/op")
		b.ReportMetric(float64(report.Missed), "missed/op")
	}
}

func mustBenchmarkWorkload(b *testing.B, rps uint64, duration time.Duration) *Workload {
	b.Helper()
	workload, err := NewWorkload(Spec{
		Duration: duration,
		Endpoints: map[string]Endpoint{
			"one": mustEndpoint(b, testClient(func(context.Context, testRequest) testResult { return testResult{} }), testProvider{}, &testCollector{}),
		},
		Phases: []Phase{{Duration: duration, RPS: rps, Targets: []Target{{Endpoint: "one", Weight: 1}}}},
	})
	if err != nil {
		b.Fatal(err)
	}
	return workload
}
