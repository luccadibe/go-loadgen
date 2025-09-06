package go_loadgen

import (
	"testing"
	"time"
)

func TestNewWorkloadPatternGenerator(t *testing.T) {
	patterns := map[string]*PhasePattern{
		"test": {
			Endpoint:           "test",
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
	patterns := map[string]*PhasePattern{
		"test": {
			Endpoint:           "test",
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
	workload := generator.GenerateWorkload()

	if len(workload) != 2 {
		t.Errorf("Expected 2 phases, got: %d", len(workload))
	}

	// Check first phase
	phase1 := workload[0]
	if phase1.Endpoint != "test" {
		t.Errorf("Expected endpoint 'test', got: %s", phase1.Endpoint)
	}
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
	patterns := map[string]*PhasePattern{
		"endpoint1": {
			Endpoint:           "endpoint1",
			PhaseCount:         IntRange{Min: 1, Max: 1},
			ConstantLikelihood: 1.0,
			RampingLikelihood:  0.0,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 1, Max: 1},
				EndRPS:   IntRange{Min: 10, Max: 10},
				Step:     IntRange{Min: 1, Max: 1},
			},
		},
		"endpoint2": {
			Endpoint:           "endpoint2",
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
	workload := generator.GenerateWorkload()

	if len(workload) != 2 {
		t.Errorf("Expected 2 phases (1 per pattern), got: %d", len(workload))
	}

	// Check that we have phases for both endpoints
	endpoints := make(map[string]bool)
	for _, phase := range workload {
		endpoints[phase.Endpoint] = true
	}

	if !endpoints["endpoint1"] {
		t.Error("Expected phase for endpoint1")
	}
	if !endpoints["endpoint2"] {
		t.Error("Expected phase for endpoint2")
	}
}

func TestWorkloadPatternGenerator_GenerateWorkload_VariablePhaseTypes(t *testing.T) {
	patterns := map[string]*PhasePattern{
		"test": {
			Endpoint:           "test",
			PhaseCount:         IntRange{Min: 1, Max: 1},
			ConstantLikelihood: 0.0, // Never constant
			RampingLikelihood:  1.0, // Always variable
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 1, Max: 1},
				EndRPS:   IntRange{Min: 10, Max: 10},
				Step:     IntRange{Min: 1, Max: 1},
			},
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 10*time.Second, patterns)
	workload := generator.GenerateWorkload()

	if len(workload) != 1 {
		t.Errorf("Expected 1 phase, got: %d", len(workload))
	}

	phase := workload[0]
	if phase.Type != "variable" {
		t.Errorf("Expected type 'variable', got: %s", phase.Type)
	}
}

func TestWorkloadPatternGenerator_GenerateWorkload_RandomSeed(t *testing.T) {
	patterns := map[string]*PhasePattern{
		"test": {
			Endpoint:           "test",
			PhaseCount:         IntRange{Min: 1, Max: 5}, // Variable count
			ConstantLikelihood: 0.5,
			RampingLikelihood:  0.5,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 1, Max: 10},
				EndRPS:   IntRange{Min: 10, Max: 20},
				Step:     IntRange{Min: 1, Max: 5},
			},
		},
	}

	// Generate workload with same seed should produce same result
	generator1 := NewWorkloadPatternGenerator(12345, 10*time.Second, patterns)
	workload1 := generator1.GenerateWorkload()

	generator2 := NewWorkloadPatternGenerator(12345, 10*time.Second, patterns)
	workload2 := generator2.GenerateWorkload()

	if len(workload1) != len(workload2) {
		t.Errorf("Expected same workload length with same seed, got: %d vs %d", len(workload1), len(workload2))
	}

	// Compare first phase details (should be identical with same seed)
	if len(workload1) > 0 && len(workload2) > 0 {
		p1, p2 := workload1[0], workload2[0]
		if p1.Type != p2.Type || p1.StartRPS != p2.StartRPS || p1.Duration != p2.Duration {
			t.Error("Expected identical phases with same seed")
		}
	}

	// Generate workload with different seed should produce different result
	generator3 := NewWorkloadPatternGenerator(54321, 10*time.Second, patterns)
	workload3 := generator3.GenerateWorkload()

	// With different seeds, at least something should be different
	// (This is probabilistic, but with good ranges it should be different)
	identical := len(workload1) == len(workload3)
	if identical && len(workload1) > 0 && len(workload3) > 0 {
		p1, p3 := workload1[0], workload3[0]
		identical = p1.Type == p3.Type && p1.StartRPS == p3.StartRPS && p1.Duration == p3.Duration
	}

	if identical {
		t.Log("Warning: Different seeds produced identical workloads (this could happen by chance)")
	}
}

func TestWorkloadPatternGenerator_GetRandInt(t *testing.T) {
	generator := NewWorkloadPatternGenerator(12345, 10*time.Second, nil)

	// Test with same min/max
	val := generator.getRandInt(5, 5)
	if val != 5 {
		t.Errorf("Expected 5 when min==max==5, got: %d", val)
	}

	// Test range
	for i := 0; i < 100; i++ {
		val := generator.getRandInt(1, 10)
		if val < 1 || val > 10 {
			t.Errorf("Value %d out of range [1, 10]", val)
		}
	}
}

func TestWorkloadPatternGenerator_PhaseNaming(t *testing.T) {
	patterns := map[string]*PhasePattern{
		"test": {
			Endpoint:           "test-endpoint",
			PhaseCount:         IntRange{Min: 3, Max: 3}, // Exactly 3 phases
			ConstantLikelihood: 1.0,
			RampingLikelihood:  0.0,
			Parameters: PhaseParameters{
				StartRPS: IntRange{Min: 1, Max: 1},
				EndRPS:   IntRange{Min: 10, Max: 10},
				Step:     IntRange{Min: 1, Max: 1},
			},
		},
	}

	generator := NewWorkloadPatternGenerator(12345, 10*time.Second, patterns)
	workload := generator.GenerateWorkload()

	if len(workload) != 3 {
		t.Fatalf("Expected 3 phases, got: %d", len(workload))
	}

	// Check phase names are correctly generated
	expectedNames := []string{"test-endpoint_0", "test-endpoint_1", "test-endpoint_2"}
	for i, phase := range workload {
		if phase.Name != expectedNames[i] {
			t.Errorf("Expected phase name '%s', got: '%s'", expectedNames[i], phase.Name)
		}
		if phase.Endpoint != "test-endpoint" {
			t.Errorf("Expected endpoint 'test-endpoint', got: '%s'", phase.Endpoint)
		}
	}
}

func TestIntRange_Validation(t *testing.T) {
	// This is more of a documentation test - IntRange should work with min > max
	// The getRandInt function handles this case
	generator := NewWorkloadPatternGenerator(12345, 10*time.Second, nil)

	// Test with min > max (should swap internally or handle gracefully)
	val := generator.getRandInt(10, 5)
	if val < 5 || val > 10 {
		t.Errorf("Value %d should be in range [5, 10] even when min > max", val)
	}
}
