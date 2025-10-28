package go_loadgen

import (
	"testing"
	"time"
)

func TestNewWorkloadPatternGenerator(t *testing.T) {
	patterns := []*PhasePattern{
		{
			Name:               "test",
			PhaseCount:         IntRange{Min: 1, Max: 3},
			ConstantLikelihood: 0.5,
			RampingLikelihood:  0.5,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 1, Max: 10},
				EndRPS:   IntRange{Min: 10, Max: 20},
				Step:     IntRange{Min: 1, Max: 5},
			},
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 10*time.Second, patterns)

	if generator.seed != 12345 {
		t.Errorf("Expected seed 12345, got: %d", generator.seed)
	}

	if generator.maxDuration != 10*time.Second {
		t.Errorf("Expected maxDuration 10s, got: %v", generator.maxDuration)
	}

	if len(generator.patterns) != 1 {
		t.Errorf("Expected 1 pattern, got: %d", len(generator.patterns))
	}
}

func TestWorkloadPatternGenerator_GenerateWorkload_SinglePattern(t *testing.T) {
	patterns := []*PhasePattern{
		{
			Name:               "test",
			PhaseCount:         IntRange{Min: 2, Max: 2}, // Exactly 2 phases
			ConstantLikelihood: 1.0,                      // Always constant
			RampingLikelihood:  0.0,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 5, Max: 5},   // Exactly 5
				EndRPS:   IntRange{Min: 10, Max: 10}, // Exactly 10
				Step:     IntRange{Min: 1, Max: 1},   // Exactly 1
			},
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 10*time.Second, patterns)
	workload, err := generator.GenerateWorkload()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(workload) != 2 {
		t.Errorf("Expected 2 phases, got: %d", len(workload))
	}

	// Check first phase
	phase1 := workload[0]

	if phase1.Type != "constant" {
		t.Errorf("Expected type 'constant', got: %s", phase1.Type)
	}
	if phase1.StartTime != 0 {
		t.Errorf("Expected StartTime 0, got: %v", phase1.StartTime)
	}
	if phase1.Duration != 5*time.Second { // 10s / 2 phases
		t.Errorf("Expected Duration 5s, got: %v", phase1.Duration)
	}
	if phase1.StartRPS != 5 {
		t.Errorf("Expected StartRPS 5, got: %d", phase1.StartRPS)
	}

	// Check second phase
	phase2 := workload[1]
	if phase2.StartTime != 5*time.Second {
		t.Errorf("Expected StartTime 5s, got: %v", phase2.StartTime)
	}
	if phase2.Duration != 5*time.Second {
		t.Errorf("Expected Duration 5s, got: %v", phase2.Duration)
	}
}

func TestWorkloadPatternGenerator_GenerateWorkload_MultiplePatterns(t *testing.T) {
	patterns := []*PhasePattern{
		{
			Name:               "endpoint1",
			PhaseCount:         IntRange{Min: 1, Max: 1},
			ConstantLikelihood: 1.0,
			RampingLikelihood:  0.0,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 1, Max: 1},
				EndRPS:   IntRange{Min: 10, Max: 10},
				Step:     IntRange{Min: 1, Max: 1},
			},
		},
		{
			Name:               "endpoint2",
			PhaseCount:         IntRange{Min: 1, Max: 1},
			ConstantLikelihood: 1.0,
			RampingLikelihood:  0.0,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 2, Max: 2},
				EndRPS:   IntRange{Min: 20, Max: 20},
				Step:     IntRange{Min: 2, Max: 2},
			},
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 10*time.Second, patterns)
	workload, err := generator.GenerateWorkload()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(workload) != 2 {
		t.Errorf("Expected 2 phases (1 per pattern), got: %d", len(workload))
	}

	// Check that we have phases for both endpoints
	endpoints := make(map[string]bool)
	for _, phase := range workload {
		endpoints[phase.Name] = true
	}

	if !endpoints["endpoint1_0"] {
		t.Error("Expected phase for endpoint1_0")
	}
	if !endpoints["endpoint2_0"] {
		t.Error("Expected phase for endpoint2_0")
	}
}

func TestWorkloadPatternGenerator_GenerateWorkload_DeterministicOrder(t *testing.T) {
	patterns := []*PhasePattern{
		{
			Name:               "first",
			PhaseCount:         IntRange{Min: 1, Max: 1},
			ConstantLikelihood: 1.0,
			RampingLikelihood:  0.0,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 1, Max: 1},
				EndRPS:   IntRange{Min: 1, Max: 1},
				Step:     IntRange{Min: 1, Max: 1},
			},
		},
		{
			Name:               "second",
			PhaseCount:         IntRange{Min: 1, Max: 1},
			ConstantLikelihood: 1.0,
			RampingLikelihood:  0.0,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 2, Max: 2},
				EndRPS:   IntRange{Min: 2, Max: 2},
				Step:     IntRange{Min: 1, Max: 1},
			},
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 20*time.Second, patterns)
	workload, err := generator.GenerateWorkload()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(workload) != 2 {
		t.Errorf("Expected 2 phases, got: %d", len(workload))
	}

	// Check that phases are in the correct order
	if workload[0].Name != "first_0" {
		t.Errorf("Expected first phase to be 'first_0', got: %s", workload[0].Name)
	}
	if workload[1].Name != "second_0" {
		t.Errorf("Expected second phase to be 'second_0', got: %s", workload[1].Name)
	}

	// Check that first phase starts at 0 and second phase starts after first ends
	if workload[0].StartTime != 0 {
		t.Errorf("Expected first phase to start at 0, got: %v", workload[0].StartTime)
	}
	expectedSecondStart := workload[0].Duration
	if workload[1].StartTime != expectedSecondStart {
		t.Errorf("Expected second phase to start at %v, got: %v", expectedSecondStart, workload[1].StartTime)
	}
}

func TestWorkloadPatternGenerator_GenerateWorkload_WithWeights(t *testing.T) {
	patterns := []*PhasePattern{
		{
			Name:               "heavy_load",
			Weight:             0.7, // 70% of time
			PhaseCount:         IntRange{Min: 2, Max: 2},
			ConstantLikelihood: 1.0,
			RampingLikelihood:  0.0,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 10, Max: 10},
				EndRPS:   IntRange{Min: 20, Max: 20},
				Step:     IntRange{Min: 1, Max: 1},
			},
		},
		{
			Name:               "light_load",
			Weight:             0.3, // 30% of time
			PhaseCount:         IntRange{Min: 1, Max: 1},
			ConstantLikelihood: 1.0,
			RampingLikelihood:  0.0,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 1, Max: 1},
				EndRPS:   IntRange{Min: 5, Max: 5},
				Step:     IntRange{Min: 1, Max: 1},
			},
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 100*time.Second, patterns)
	workload, err := generator.GenerateWorkload()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(workload) != 3 { // 2 phases for heavy_load + 1 phase for light_load
		t.Errorf("Expected 3 phases total, got: %d", len(workload))
	}

	// Check that heavy_load phases take up 70% of the time
	heavyLoadDuration := time.Duration(0)
	lightLoadDuration := time.Duration(0)

	for _, phase := range workload {
		if phase.Name == "heavy_load_0" || phase.Name == "heavy_load_1" {
			heavyLoadDuration += phase.Duration
		} else if phase.Name == "light_load_0" {
			lightLoadDuration += phase.Duration
		}
	}

	expectedHeavyDuration := 70 * time.Second
	expectedLightDuration := 30 * time.Second

	if heavyLoadDuration != expectedHeavyDuration {
		t.Errorf("Expected heavy_load duration %v, got: %v", expectedHeavyDuration, heavyLoadDuration)
	}
	if lightLoadDuration != expectedLightDuration {
		t.Errorf("Expected light_load duration %v, got: %v", expectedLightDuration, lightLoadDuration)
	}
}

func TestWorkloadPatternGenerator_CalculatePatternTimes_EqualWeights(t *testing.T) {
	patterns := []*PhasePattern{
		{
			Name:   "pattern1",
			Weight: 0.0, // No weight specified
		},
		{
			Name:   "pattern2",
			Weight: 0.0, // No weight specified
		},
		{
			Name:   "pattern3",
			Weight: 0.0, // No weight specified
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 30*time.Second, patterns)
	err := generator.calculatePatternTimes()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	// Should split evenly between 3 patterns
	expectedTime := 10 * time.Second
	for _, pattern := range generator.patterns {
		if pattern.totalTime != expectedTime {
			t.Errorf("Pattern %s expected totalTime %v, got: %v", pattern.Name, expectedTime, pattern.totalTime)
		}
	}
}

func TestWorkloadPatternGenerator_CalculatePatternTimes_CustomWeights(t *testing.T) {
	patterns := []*PhasePattern{
		{
			Name:   "pattern1",
			Weight: 0.5, // 50% of time
		},
		{
			Name:   "pattern2",
			Weight: 0.3, // 30% of time
		},
		{
			Name:   "pattern3",
			Weight: 0.2, // 20% of time
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 100*time.Second, patterns)
	err := generator.calculatePatternTimes()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	expectedTimes := map[string]time.Duration{
		"pattern1": 50 * time.Second, // 50%
		"pattern2": 30 * time.Second, // 30%
		"pattern3": 20 * time.Second, // 20%
	}

	for _, pattern := range generator.patterns {
		expected := expectedTimes[pattern.Name]
		if pattern.totalTime != expected {
			t.Errorf("Pattern %s expected totalTime %v, got: %v", pattern.Name, expected, pattern.totalTime)
		}
	}
}

func TestWorkloadPatternGenerator_CalculatePatternTimes_InvalidWeights(t *testing.T) {
	patterns := []*PhasePattern{
		{
			Name:   "pattern1",
			Weight: 0.5,
		},
		{
			Name:   "pattern2",
			Weight: 0.3,
		},
		{
			Name:   "pattern3",
			Weight: 0.1, // Total is 0.9, not 1.0
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 100*time.Second, patterns)
	err := generator.calculatePatternTimes()
	if err == nil {
		t.Error("Expected error for weights that don't sum to 1.0")
	}
	if err.Error() != "pattern weights must sum to 1.0" {
		t.Errorf("Expected specific error message, got: %v", err)
	}
}

func TestWorkloadPatternGenerator_EmptyPatterns(t *testing.T) {
	patterns := []*PhasePattern{}

	generator := NewWorkloadPatternGenerator(12345, 100*time.Second, patterns)
	err := generator.calculatePatternTimes()
	if err != nil {
		t.Errorf("Expected no error for empty patterns, got: %v", err)
	}

	workload, err := generator.GenerateWorkload()
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if len(workload) != 0 {
		t.Errorf("Expected empty workload, got: %d phases", len(workload))
	}
}
