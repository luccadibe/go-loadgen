package go_loadgen

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Test data structures
type testRequest struct {
	ID int
}

type testResponse struct {
	ID      int
	Latency time.Duration
	Error   string
}

// Mock client for testing
type mockClient struct {
	callCount int
	mu        sync.Mutex
	delay     time.Duration
	shouldErr bool
}

func (c *mockClient) CallEndpoint(ctx context.Context, req testRequest) testResponse {
	c.mu.Lock()
	c.callCount++
	c.mu.Unlock()

	if c.delay > 0 {
		time.Sleep(c.delay)
	}

	if c.shouldErr {
		return testResponse{ID: req.ID, Error: "test error"}
	}

	return testResponse{ID: req.ID, Latency: c.delay}
}

func (c *mockClient) GetCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCount
}

// Mock data provider for testing
type mockDataProvider struct {
	counter int
	mu      sync.Mutex
}

func (d *mockDataProvider) GetData() testRequest {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.counter++
	return testRequest{ID: d.counter}
}

// Mock collector for testing
type mockCollector struct {
	results []testResponse
	mu      sync.Mutex
	closed  bool
}

func (c *mockCollector) Collect(result testResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.results = append(c.results, result)
}

func (c *mockCollector) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
}

func (c *mockCollector) GetResults() []testResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]testResponse{}, c.results...)
}

func (c *mockCollector) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func TestNewEndpointWorkload_ValidConfig(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	config := &Config{
		GenerateWorkload: false,
		MaxDuration:      10 * time.Second,
		Phases: []TestPhase{
			{
				Name:      "test",
				Type:      "constant",
				StartTime: 0,
				Duration:  2 * time.Second,
				StartRPS:  1,
			},
		},
	}

	workload, err := NewEndpointWorkload("test", config, client, dataProvider, collector)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if workload.Name != "test" {
		t.Errorf("Expected name 'test', got: %s", workload.Name)
	}
}

func TestNewEndpointWorkload_NoPhases(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	config := &Config{
		GenerateWorkload: false,
		MaxDuration:      10 * time.Second,
		Phases:           []TestPhase{}, // Empty phases
	}

	_, err := NewEndpointWorkload("test", config, client, dataProvider, collector)
	if err == nil {
		t.Fatal("Expected error for empty phases, got nil")
	}

	expectedError := "workload generation is disabled but no phases provided"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got: %s", expectedError, err.Error())
	}
}

func TestNewEndpointWorkload_GenerateWorkloadNoPatterns(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	config := &Config{
		GenerateWorkload: true,
		MaxDuration:      10 * time.Second,
		Patterns:         nil, // No patterns provided
	}

	_, err := NewEndpointWorkload("test", config, client, dataProvider, collector)
	if err == nil {
		t.Fatal("Expected error for missing patterns, got nil")
	}

	expectedError := "workload generation is enabled but no patterns provided"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got: %s", expectedError, err.Error())
	}
}

func TestNewEndpointWorkload_GenerateWorkload(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	config := &Config{
		GenerateWorkload: true,
		MaxDuration:      10 * time.Second,
		Seed:             12345,
		Patterns: []*PhasePattern{
			{
				Name:               "test",
				PhaseCount:         IntRange{Min: 2, Max: 3},
				ConstantLikelihood: 1.0, // Always constant
				RampingLikelihood:  0.0,
				Parameters: PhaseParameters{
					StartRPS: IntRange{Min: 1, Max: 5},
					EndRPS:   IntRange{Min: 10, Max: 20},
					Step:     IntRange{Min: 1, Max: 2},
				},
			},
		},
	}

	workload, err := NewEndpointWorkload("test", config, client, dataProvider, collector)
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(workload.Config.Phases) < 2 || len(workload.Config.Phases) > 3 {
		t.Errorf("Expected 2-3 phases, got: %d", len(workload.Config.Phases))
	}

	// Verify phases have correct properties
	for _, phase := range workload.Config.Phases {
		if phase.Type != "constant" {
			t.Errorf("Expected constant phase type, got: %s", phase.Type)
		}
		if phase.StartRPS < 1 || phase.StartRPS > 5 {
			t.Errorf("StartRPS out of range: %d", phase.StartRPS)
		}
	}
}

func TestEndpointWorkload_Run_ConstantPhase(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	config := &Config{
		GenerateWorkload: false,
		MaxDuration:      5 * time.Second,
		Phases: []TestPhase{
			{
				Name:      "test",
				Type:      "constant",
				StartTime: 0,
				Duration:  2 * time.Second,
				StartRPS:  2, // 2 requests per second
			},
		},
	}

	workload, err := NewEndpointWorkload("test", config, client, dataProvider, collector)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	start := time.Now()
	workload.Run()
	elapsed := time.Since(start)

	// Should run for approximately 2 seconds
	if elapsed < 1800*time.Millisecond || elapsed > 3*time.Second {
		t.Errorf("Expected ~2s execution time, got: %v", elapsed)
	}

	// Should have made approximately 4 calls (2 RPS * 2 seconds)
	callCount := client.GetCallCount()
	if callCount < 3 || callCount > 5 {
		t.Errorf("Expected ~4 calls, got: %d", callCount)
	}

	// Should have collected results
	results := collector.GetResults()
	if len(results) != callCount {
		t.Errorf("Expected %d results, got: %d", callCount, len(results))
	}
}

func TestEndpointWorkload_Run_MultiplePhases(t *testing.T) {
	client := &mockClient{}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	config := &Config{
		GenerateWorkload: false,
		MaxDuration:      10 * time.Second,
		Phases: []TestPhase{
			{
				Name:      "phase1",
				Type:      "constant",
				StartTime: 0,
				Duration:  1 * time.Second,
				StartRPS:  1,
			},
			{
				Name:      "phase2",
				Type:      "constant",
				StartTime: 500 * time.Millisecond, // Overlapping phases
				Duration:  1 * time.Second,
				StartRPS:  1,
			},
		},
	}

	workload, err := NewEndpointWorkload("test", config, client, dataProvider, collector)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	start := time.Now()
	workload.Run()
	elapsed := time.Since(start)

	// Should run for approximately 1.5 seconds (phase2 starts at 0.5s and runs for 1s)
	if elapsed < 1200*time.Millisecond || elapsed > 2500*time.Millisecond {
		t.Errorf("Expected ~1.5s execution time, got: %v", elapsed)
	}

	// Should have made calls from both phases
	callCount := client.GetCallCount()
	if callCount < 2 {
		t.Errorf("Expected at least 2 calls from overlapping phases, got: %d", callCount)
	}
}

func TestEndpointWorkload_Run_ContextTimeout(t *testing.T) {
	// Client with delay longer than MaxDuration
	client := &mockClient{delay: 100 * time.Millisecond}
	dataProvider := &mockDataProvider{}
	collector := &mockCollector{}

	config := &Config{
		GenerateWorkload: false,
		MaxDuration:      500 * time.Millisecond, // Short max duration
		Phases: []TestPhase{
			{
				Name:      "test",
				Type:      "constant",
				StartTime: 0,
				Duration:  2 * time.Second, // Longer than MaxDuration
				StartRPS:  10,
			},
		},
	}

	workload, err := NewEndpointWorkload("test", config, client, dataProvider, collector)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	start := time.Now()
	workload.Run()
	elapsed := time.Since(start)

	// Should respect MaxDuration timeout
	if elapsed > 1*time.Second {
		t.Errorf("Expected execution to stop due to MaxDuration, took: %v", elapsed)
	}
}
