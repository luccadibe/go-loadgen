package go_loadgen

import (
	"context"
	"testing"
	"time"
)

const (
	benchmarkRPS      = 10000
	benchmarkDuration = 100 * time.Millisecond
	benchmarkStartRPS = 50
	benchmarkEndRPS   = 150
	benchmarkStep     = 10
)

type benchmarkRequest struct {
	ID int
}

type benchmarkResponse struct {
	ID int
}

type benchmarkClient struct {
}

func (c *benchmarkClient) CallEndpoint(ctx context.Context, req benchmarkRequest) benchmarkResponse {
	return benchmarkResponse(req)
}

type benchmarkDataProvider struct {
}

func (d *benchmarkDataProvider) GetData() benchmarkRequest {
	return benchmarkRequest{ID: 122126621617}
}

type benchmarkCollector struct {
}

func (c *benchmarkCollector) Collect(result benchmarkResponse) {
	// No-op for benchmarking
}

func (c *benchmarkCollector) Close() {
	// No-op for benchmarking
}

func BenchmarkConstantExecutor_Execute(b *testing.B) {
	client := &benchmarkClient{}
	dataProvider := &benchmarkDataProvider{}
	collector := &benchmarkCollector{}

	phase := TestPhase{
		Name:     "benchmark",
		Type:     "constant",
		Duration: benchmarkDuration,
		StartRPS: benchmarkRPS,
	}

	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		// Create a new executor for each iteration to avoid state pollution
		executor := NewConstantExecutor(client, collector, dataProvider)
		executor.Execute(ctx, phase)
	}
}

func BenchmarkRampingExecutor_Execute(b *testing.B) {
	client := &benchmarkClient{}
	dataProvider := &benchmarkDataProvider{}
	collector := &benchmarkCollector{}

	phase := TestPhase{
		Name:     "benchmark",
		Type:     "variable",
		Duration: benchmarkDuration,
		StartRPS: benchmarkStartRPS,
		EndRPS:   benchmarkEndRPS,
		Step:     benchmarkStep,
	}

	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		// Create a new executor for each iteration to avoid state pollution
		executor := NewRampingExecutor(client, collector, dataProvider)
		executor.Execute(ctx, phase)
	}
}

func BenchmarkConstantExecutor_Execute_HighRPS(b *testing.B) {
	client := &benchmarkClient{}
	dataProvider := &benchmarkDataProvider{}
	collector := &benchmarkCollector{}

	const highRPS = 1000000
	phase := TestPhase{
		Name:     "benchmark",
		Type:     "constant",
		Duration: benchmarkDuration,
		StartRPS: highRPS,
	}

	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		executor := NewConstantExecutor(client, collector, dataProvider)
		executor.Execute(ctx, phase)
	}
}

func BenchmarkRampingExecutor_Execute_LargeRamp(b *testing.B) {
	client := &benchmarkClient{}
	dataProvider := &benchmarkDataProvider{}
	collector := &benchmarkCollector{}

	const (
		largeStartRPS = 10000
		largeEndRPS   = 50000
		largeStep     = 500
	)

	phase := TestPhase{
		Name:     "benchmark",
		Type:     "variable",
		Duration: benchmarkDuration,
		StartRPS: largeStartRPS,
		EndRPS:   largeEndRPS,
		Step:     largeStep,
	}

	ctx := context.Background()

	b.ReportAllocs()

	for b.Loop() {
		executor := NewRampingExecutor(client, collector, dataProvider)
		executor.Execute(ctx, phase)
	}
}
