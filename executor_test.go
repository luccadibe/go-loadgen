package go_loadgen

import (
	"context"
	"testing"
	"time"
)

func TestConstantExecutor_Execute(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	executor := NewConstantExecutor(client, collector, dataProvider)

	phase := TestPhase{
		Name:     "test",
		Type:     "constant",
		Duration: 2 * time.Second,
		StartRPS: 3, // 3 requests per second
	}

	ctx := context.Background()
	start := time.Now()
	executor.Execute(ctx, phase)
	elapsed := time.Since(start)

	// Should run for approximately 2 seconds
	if elapsed < 1800*time.Millisecond || elapsed > 2500*time.Millisecond {
		t.Errorf("Expected ~2s execution time, got: %v", elapsed)
	}

	// Give some time for in-flight goroutines to complete
	time.Sleep(100 * time.Millisecond)

	// Should have made approximately 6 calls (3 RPS * 2 seconds)
	// But could be less due to timing (first tick delay) or more due to in-flight goroutines
	callCount := client.GetCallCount()
	if callCount < 3 || callCount > 10 {
		t.Errorf("Expected 3-10 calls (allowing for timing variations), got: %d", callCount)
	}

	// Should have collected results (may be more than calls due to async nature)
	results := collector.GetResults()
	if len(results) < callCount {
		t.Errorf("Expected at least %d results, got: %d", callCount, len(results))
	}
}

func TestConstantExecutor_Stop(t *testing.T) {
	client := &mockClient{delay: 100 * time.Millisecond}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	executor := NewConstantExecutor(client, collector, dataProvider)

	phase := TestPhase{
		Name:     "test",
		Type:     "constant",
		Duration: 5 * time.Second, // Long duration
		StartRPS: 1,
	}

	ctx := context.Background()

	// Start execution in goroutine
	done := make(chan bool)
	go func() {
		executor.Execute(ctx, phase)
		done <- true
	}()

	// Wait a bit then stop
	time.Sleep(200 * time.Millisecond)
	executor.Stop()

	// Should stop quickly
	select {
	case <-done:
		// Good, stopped
	case <-time.After(1 * time.Second):
		t.Error("Executor did not stop within timeout")
	}
}

func TestRampingExecutor_Execute_Increment(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	executor := NewRampingExecutor(client, collector, dataProvider)

	phase := TestPhase{
		Name:     "test",
		Type:     "variable",
		Duration: 3 * time.Second,
		StartRPS: 1,
		EndRPS:   3,
		Step:     1, // Increment by 1 each second
	}

	ctx := context.Background()
	start := time.Now()
	executor.Execute(ctx, phase)
	elapsed := time.Since(start)

	// Should run for approximately 3 seconds
	if elapsed < 2800*time.Millisecond || elapsed > 3500*time.Millisecond {
		t.Errorf("Expected ~3s execution time, got: %v", elapsed)
	}

	// Give some time for in-flight goroutines to complete
	time.Sleep(100 * time.Millisecond)

	// Should ramp from 1 to 3 RPS over 3 seconds
	// Second 1: 1 request, Second 2: 2 requests, Second 3: 3 requests = ~6 total
	// But timing variations and in-flight goroutines can affect this
	callCount := client.GetCallCount()
	if callCount < 3 || callCount > 10 {
		t.Errorf("Expected 3-10 calls (ramping 1->3, allowing for timing variations), got: %d", callCount)
	}
}

func TestRampingExecutor_Execute_Decrement(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	executor := NewRampingExecutor(client, collector, dataProvider)

	phase := TestPhase{
		Name:     "test",
		Type:     "variable",
		Duration: 3 * time.Second,
		StartRPS: 3,
		EndRPS:   1,
		Step:     -1, // Decrement by 1 each second
	}

	ctx := context.Background()
	start := time.Now()
	executor.Execute(ctx, phase)
	elapsed := time.Since(start)

	// Should run for approximately 3 seconds
	if elapsed < 2800*time.Millisecond || elapsed > 3500*time.Millisecond {
		t.Errorf("Expected ~3s execution time, got: %v", elapsed)
	}

	// Give some time for in-flight goroutines to complete
	time.Sleep(100 * time.Millisecond)

	// Should ramp from 3 to 1 RPS over 3 seconds
	// Second 1: 3 requests, Second 2: 2 requests, Second 3: 1 request = ~6 total
	// But timing variations and in-flight goroutines can affect this
	callCount := client.GetCallCount()
	if callCount < 3 || callCount > 9 {
		t.Errorf("Expected 3-9 calls (ramping 3->1, allowing for timing variations), got: %d", callCount)
	}
}

func TestRampingExecutor_Execute_ZeroStartRPS(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	executor := NewRampingExecutor(client, collector, dataProvider)

	phase := TestPhase{
		Name:     "test",
		Type:     "variable",
		Duration: 2 * time.Second,
		StartRPS: 0, // Zero start should be handled
		EndRPS:   2,
		Step:     1,
	}

	ctx := context.Background()
	executor.Execute(ctx, phase)

	callCount := client.GetCallCount()
	if callCount < 1 {
		t.Errorf("Expected at least 1 call with zero start RPS, got: %d", callCount)
	}
}

func TestRampingExecutor_Execute_NegativeRPS(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	executor := NewRampingExecutor(client, collector, dataProvider)

	phase := TestPhase{
		Name:     "test",
		Type:     "variable",
		Duration: 2 * time.Second,
		StartRPS: 2,
		EndRPS:   0,
		Step:     -3, // Large negative step that could go negative
	}

	ctx := context.Background()
	start := time.Now()
	executor.Execute(ctx, phase)
	elapsed := time.Since(start)

	if elapsed < 1800*time.Millisecond || elapsed > 2500*time.Millisecond {
		t.Errorf("Expected ~2s execution time even with negative RPS, got: %v", elapsed)
	}

	callCount := client.GetCallCount()
	if callCount < 1 {
		t.Errorf("Expected at least some calls before going negative, got: %d", callCount)
	}
}

func TestRampingExecutor_Stop(t *testing.T) {
	client := &mockClient{delay: 100 * time.Millisecond}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	executor := NewRampingExecutor(client, collector, dataProvider)

	phase := TestPhase{
		Name:     "test",
		Type:     "variable",
		Duration: 5 * time.Second,
		StartRPS: 1,
		EndRPS:   5,
		Step:     1,
	}

	ctx := context.Background()

	done := make(chan bool)
	go func() {
		executor.Execute(ctx, phase)
		done <- true
	}()

	// Wait a bit then stop
	time.Sleep(200 * time.Millisecond)
	executor.Stop()

	// Should stop quickly
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Error("Executor did not stop within timeout")
	}
}

func TestRampingExecutor_Execute_ContextCancellation(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	executor := NewRampingExecutor(client, collector, dataProvider)

	phase := TestPhase{
		Name:     "test",
		Type:     "variable",
		Duration: 5 * time.Second, // Long duration
		StartRPS: 1,
		EndRPS:   5,
		Step:     1,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	start := time.Now()
	executor.Execute(ctx, phase)
	elapsed := time.Since(start)

	// Should respect context timeout
	if elapsed > 1*time.Second {
		t.Errorf("Expected execution to stop due to context timeout, took: %v", elapsed)
	}
}

func TestCalculateInterval(t *testing.T) {
	tests := []struct {
		rps             int
		interval        time.Duration
		expectedRestRPS int
	}{
		{4, 250 * time.Millisecond, 1},
		{10, 100 * time.Millisecond, 1},
		{20, 50 * time.Millisecond, 1},
		{50, 20 * time.Millisecond, 1},
		// limit
		{100_000_000, 10 * time.Millisecond, 100_000_000 / 100},
		{10_000_000, 10 * time.Millisecond, 10_000_000 / 100},
	}

	for _, test := range tests {
		interval, restRPS := calculateInterval(test.rps)
		if interval != test.interval {
			t.Errorf("Expected %v interval, got: %v", test.interval, interval)
		}
		if restRPS != test.expectedRestRPS {
			t.Errorf("Expected %d restRPS, got: %d", test.expectedRestRPS, restRPS)
		}
	}
}
